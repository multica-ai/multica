package mapcollector

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Collector struct {
	contract       *Contract
	contractBytes  []byte
	key            []byte
	attachmentsDir string
	records        map[string][]record
	report         Report
}

func Collect(ctx context.Context, pool *pgxpool.Pool, contract *Contract, contractBytes, key []byte, attachmentsDir string) (*Report, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("HMAC key must contain at least 32 bytes")
	}
	c := &Collector{
		contract:       contract,
		contractBytes:  contractBytes,
		key:            key,
		attachmentsDir: attachmentsDir,
		records:        make(map[string][]record),
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin read-only snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := c.initializeReport(ctx, tx); err != nil {
		return nil, err
	}
	if err := c.validateSchema(ctx, tx); err != nil {
		return nil, err
	}
	if err := c.collectTables(ctx, tx); err != nil {
		return nil, err
	}
	c.collectDomains()
	c.collectUniqueViolations()
	c.collectReferences()
	c.collectOwners()
	c.collectPermissions()
	if err := c.collectAttachments(); err != nil {
		return nil, err
	}
	c.collectUsage()
	c.collectTasks()
	c.report.Accepted = len(c.report.Rejections) == 0
	return &c.report, nil
}

func (c *Collector) initializeReport(ctx context.Context, tx pgx.Tx) error {
	var versionNumber string
	if err := tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::text`).Scan(&versionNumber); err != nil {
		return fmt.Errorf("read PostgreSQL version: %w", err)
	}
	if versionNumber != "170010" || c.contract.PostgresVersion != "17.10" {
		return fmt.Errorf("PostgreSQL version drift: got server_version_num %q, require 170010", versionNumber)
	}
	snapshotHash, err := c.databaseHMAC(ctx, tx, "snapshot:"+c.contract.SnapshotLabel)
	if err != nil {
		return err
	}
	expected := c.keyedDigest([]byte("snapshot:" + c.contract.SnapshotLabel))
	if !hmac.Equal(snapshotHash, expected) {
		return fmt.Errorf("typed database HMAC does not match in-process HMAC")
	}
	c.report = Report{
		MappingVersion:  MappingVersion,
		SnapshotIDHash:  hex.EncodeToString(snapshotHash),
		PostgresVersion: c.contract.PostgresVersion,
		Schema:          c.contract.Schema,
		ContractSHA256:  contractHash(c.contractBytes),
		Rejections:      []Rejection{},
	}
	return nil
}

func (c *Collector) databaseHMAC(ctx context.Context, tx pgx.Tx, value string) ([]byte, error) {
	var digest []byte
	var extensionSchema string
	if err := tx.QueryRow(ctx, `
		SELECT n.nspname::text
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = 'pgcrypto'::text
	`).Scan(&extensionSchema); err != nil {
		return nil, fmt.Errorf("resolve pgcrypto extension schema: %w", err)
	}
	// Every hmac argument and the result are explicitly typed. This avoids the
	// text/bytea overload ambiguity that stopped the original field collection.
	query := fmt.Sprintf(`
		SELECT %s.hmac(
			convert_to($1::text, 'UTF8'),
			$2::bytea,
			'sha256'::text
		)::bytea
	`, quoteIdentifier(extensionSchema))
	err := tx.QueryRow(ctx, query, value, c.key).Scan(&digest)
	if err != nil {
		return nil, fmt.Errorf("compute typed database HMAC: %w", err)
	}
	return digest, nil
}

func (c *Collector) validateSchema(ctx context.Context, tx pgx.Tx) error {
	for i := range c.contract.Tables {
		table := &c.contract.Tables[i]
		rows, err := tx.Query(ctx, `
			SELECT column_name::text, data_type::text, is_nullable::text
			  FROM information_schema.columns
			 WHERE table_schema = $1::text
			   AND table_name = $2::text
			 ORDER BY ordinal_position
		`, c.contract.Schema, table.Name)
		if err != nil {
			return fmt.Errorf("inspect table %s: %w", table.Name, err)
		}
		actual := make(map[string]schemaColumn)
		for rows.Next() {
			var name, dataType, nullable string
			if err := rows.Scan(&name, &dataType, &nullable); err != nil {
				rows.Close()
				return fmt.Errorf("scan schema for %s: %w", table.Name, err)
			}
			actual[name] = schemaColumn{dataType: dataType, nullable: nullable == "YES"}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("read schema for %s: %w", table.Name, err)
		}
		rows.Close()
		if len(actual) == 0 {
			return fmt.Errorf("schema drift: required table %s.%s is missing", c.contract.Schema, table.Name)
		}
		expectedNames := make(map[string]struct{}, len(table.Fields)+len(table.DeniedColumns))
		for _, field := range table.Fields {
			expectedNames[field.Name] = struct{}{}
			column, ok := actual[field.Name]
			if !ok {
				return fmt.Errorf("schema drift: required field %s.%s is missing", table.Name, field.Name)
			}
			if column.dataType != field.Type || column.nullable != field.Nullable {
				return fmt.Errorf("schema drift: field %s.%s is %s nullable=%t, require %s nullable=%t", table.Name, field.Name, column.dataType, column.nullable, field.Type, field.Nullable)
			}
		}
		for _, denied := range table.DeniedColumns {
			expectedNames[denied] = struct{}{}
			if _, ok := actual[denied]; !ok {
				return fmt.Errorf("schema drift: denied field %s.%s is missing", table.Name, denied)
			}
		}
		if len(actual) != len(expectedNames) {
			unknown := make([]string, 0)
			for name := range actual {
				if _, ok := expectedNames[name]; !ok {
					unknown = append(unknown, name)
				}
			}
			sort.Strings(unknown)
			return fmt.Errorf("schema drift: table %s has unclassified columns %v", table.Name, unknown)
		}
	}
	return nil
}

type schemaColumn struct {
	dataType string
	nullable bool
}

func (c *Collector) collectTables(ctx context.Context, tx pgx.Tx) error {
	for i := range c.contract.Tables {
		table := &c.contract.Tables[i]
		columns := make([]string, 0, len(table.Fields))
		for _, field := range table.Fields {
			columns = append(columns, quoteIdentifier(field.Name))
		}
		query := fmt.Sprintf("SELECT %s FROM %s.%s ORDER BY %s", strings.Join(columns, ", "), quoteIdentifier(c.contract.Schema), quoteIdentifier(table.Name), quoteIdentifier(table.IDField))
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("query allowlisted fields for %s: %w", table.Name, err)
		}
		report := TableReport{
			Domain:        table.Domain,
			Name:          table.Name,
			DeniedColumns: sortedCopy(table.DeniedColumns),
			EnumCoverage:  make(map[string]map[string]int),
		}
		for _, field := range table.Fields {
			report.Fields = append(report.Fields, FieldReport{Name: field.Name, Type: field.Type, Nullable: field.Nullable, Role: field.Role})
			if len(field.Enum) > 0 {
				report.EnumCoverage[field.Name] = make(map[string]int)
				for _, value := range field.Enum {
					report.EnumCoverage[field.Name][value] = 0
				}
			}
		}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return fmt.Errorf("read row for %s: %w", table.Name, err)
			}
			rowValues := make(map[string]any, len(table.Fields))
			for idx, field := range table.Fields {
				rowValues[field.Name] = normalizeValue(values[idx])
			}
			canonical, err := json.Marshal(rowValues)
			if err != nil {
				rows.Close()
				return fmt.Errorf("canonicalize row for %s: %w", table.Name, err)
			}
			canonical, err = jsoncanonicalizer.Transform(canonical)
			if err != nil {
				rows.Close()
				return fmt.Errorf("RFC 8785 canonicalize row for %s: %w", table.Name, err)
			}
			idValue := scalarString(rowValues[table.IDField])
			anonymousDigest := c.keyedDigest([]byte(table.Name + ":" + idValue))
			rowDigest := c.keyedDigest(canonical)
			rec := record{table: table, values: rowValues, anonymousID: hex.EncodeToString(anonymousDigest), bucket: int(anonymousDigest[0]), rowHMAC: rowDigest}
			c.records[table.Name] = append(c.records[table.Name], rec)
			report.RowCount++
			for _, field := range table.Fields {
				if len(field.Enum) == 0 || rowValues[field.Name] == nil {
					continue
				}
				value := scalarString(rowValues[field.Name])
				if _, ok := report.EnumCoverage[field.Name][value]; !ok {
					c.reject(table.Domain, table.Name, rec.anonymousID, "ENUM_UNKNOWN", "fatal", "reject", nil, false, []byte(field.Name+":"+value))
					continue
				}
				report.EnumCoverage[field.Name][value]++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate %s: %w", table.Name, err)
		}
		rows.Close()
		report.Buckets = bucketize(c.key, c.records[table.Name])
		c.report.Tables = append(c.report.Tables, report)
	}
	return nil
}

func (c *Collector) keyedDigest(value []byte) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func normalizeValue(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case []byte:
		return hex.EncodeToString(v)
	default:
		return value
	}
}

func scalarString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	}
}

func bucketize(key []byte, records []record) []BucketReport {
	buckets := make([][][]byte, 256)
	for _, rec := range records {
		buckets[rec.bucket] = append(buckets[rec.bucket], append([]byte(nil), rec.rowHMAC...))
	}
	reports := make([]BucketReport, 256)
	for i := range buckets {
		sort.Slice(buckets[i], func(a, b int) bool { return bytes.Compare(buckets[i][a], buckets[i][b]) < 0 })
		mac := hmac.New(sha256.New, key)
		for _, digest := range buckets[i] {
			_, _ = mac.Write(digest)
		}
		reports[i] = BucketReport{Bucket: i, Count: len(buckets[i]), HMAC256: hex.EncodeToString(mac.Sum(nil))}
	}
	return reports
}

func (c *Collector) collectDomains() {
	byDomain := make(map[string][]record)
	for _, records := range c.records {
		for _, rec := range records {
			byDomain[rec.table.Domain] = append(byDomain[rec.table.Domain], rec)
		}
	}
	domains := make([]string, 0, len(byDomain))
	for domain := range byDomain {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	for _, domain := range domains {
		c.report.Domains = append(c.report.Domains, DomainReport{
			Name:     domain,
			RowCount: len(byDomain[domain]),
			Buckets:  bucketize(c.key, byDomain[domain]),
		})
	}
}

func (c *Collector) collectUniqueViolations() {
	for i := range c.contract.Tables {
		table := &c.contract.Tables[i]
		for _, unique := range table.Unique {
			seen := make(map[string]string)
			for _, rec := range c.records[table.Name] {
				parts := make([]string, len(unique.Fields))
				for idx, field := range unique.Fields {
					parts[idx] = scalarString(rec.values[field])
				}
				key := strings.Join(parts, "\x00")
				if previous, ok := seen[key]; ok {
					c.reject(table.Domain, table.Name, rec.anonymousID, "IDENTITY_COLLISION", "fatal", "reject", []string{previous}, false, []byte(unique.Name+":"+key))
				} else {
					seen[key] = rec.anonymousID
				}
			}
		}
	}
}

func (c *Collector) collectReferences() {
	for _, ref := range c.contract.References {
		targets := make(map[string]record)
		for _, target := range c.records[ref.ToTable] {
			targets[recordKey(target, ref.ToFields)] = target
		}
		report := ReferenceReport{Name: ref.Name, Domain: ref.Domain}
		links := make(map[string]string)
		for _, source := range c.records[ref.FromTable] {
			report.RowsChecked++
			key, allNull, someNull := nullableRecordKey(source, ref.FromFields)
			if allNull {
				report.NullCount++
				if !ref.AllowNull {
					report.OrphanCount++
					c.reject(ref.Domain, ref.FromTable, source.anonymousID, reasonForReference(ref), "fatal", "reject", nil, false, []byte(ref.Name+":null"))
				}
				continue
			}
			if someNull {
				report.OrphanCount++
				c.reject(ref.Domain, ref.FromTable, source.anonymousID, reasonForReference(ref), "fatal", "reject", nil, false, []byte(ref.Name+":partial-null"))
				continue
			}
			target, ok := targets[key]
			if !ok {
				report.OrphanCount++
				c.reject(ref.Domain, ref.FromTable, source.anonymousID, reasonForReference(ref), "fatal", "reject", nil, false, []byte(ref.Name+":"+key))
				continue
			}
			if ref.FromWorkspaceField != "" && scalarString(source.values[ref.FromWorkspaceField]) != scalarString(target.values[ref.ToWorkspaceField]) {
				report.CrossWorkspaceCount++
				c.reject(ref.Domain, ref.FromTable, source.anonymousID, "CROSS_WORKSPACE_REF", "fatal", "reject", []string{target.anonymousID}, false, []byte(ref.Name+":"+key))
			}
			if ref.Acyclic {
				links[source.anonymousID] = target.anonymousID
			}
		}
		if ref.Acyclic {
			cycles := cycleMembers(links)
			report.CycleCount = len(cycles)
			for _, anonymousID := range cycles {
				c.reject(ref.Domain, ref.FromTable, anonymousID, "PARENT_CYCLE", "fatal", "reject", []string{links[anonymousID]}, false, []byte(ref.Name+":"+anonymousID))
			}
		}
		c.report.References = append(c.report.References, report)
	}
}

func (c *Collector) collectOwners() {
	if c.contract.Owners == nil {
		return
	}
	o := c.contract.Owners
	report := &OwnerReport{WorkspacesChecked: len(c.records[o.WorkspaceTable])}
	ownerCounts := make(map[string]int)
	for _, member := range c.records[o.MemberTable] {
		if scalarString(member.values[o.MemberRoleField]) == o.OwnerRole {
			ownerCounts[scalarString(member.values[o.MemberWorkspaceField])]++
		}
	}
	for _, workspace := range c.records[o.WorkspaceTable] {
		workspaceID := scalarString(workspace.values[o.WorkspaceIDField])
		if ownerCounts[workspaceID] == 0 {
			report.MissingOwnerCount++
			c.reject("identity_workspace", o.WorkspaceTable, workspace.anonymousID, "MISSING_OWNER", "fatal", "reject", nil, false, []byte("workspace-owner:"+workspaceID))
		}
	}
	c.report.Owners = report
}

func reasonForReference(ref ReferenceContract) string {
	name := strings.ToLower(ref.Name + " " + ref.Domain)
	if strings.Contains(name, "owner") {
		return "MISSING_OWNER"
	}
	if strings.Contains(name, "attachment") {
		return "ATTACHMENT_OBJECT_MISSING"
	}
	return "REFERENCE_ORPHAN"
}

func recordKey(rec record, fields []string) string {
	parts := make([]string, len(fields))
	for i, field := range fields {
		parts[i] = scalarString(rec.values[field])
	}
	return strings.Join(parts, "\x00")
}

func nullableRecordKey(rec record, fields []string) (string, bool, bool) {
	parts := make([]string, len(fields))
	nullCount := 0
	for i, field := range fields {
		if rec.values[field] == nil {
			nullCount++
		} else {
			parts[i] = scalarString(rec.values[field])
		}
	}
	return strings.Join(parts, "\x00"), nullCount == len(fields), nullCount > 0 && nullCount < len(fields)
}

func cycleMembers(edges map[string]string) []string {
	cycleSet := make(map[string]struct{})
	for start := range edges {
		positions := make(map[string]int)
		path := make([]string, 0)
		current := start
		for current != "" {
			if pos, ok := positions[current]; ok {
				for _, member := range path[pos:] {
					cycleSet[member] = struct{}{}
				}
				break
			}
			positions[current] = len(path)
			path = append(path, current)
			current = edges[current]
		}
	}
	cycles := make([]string, 0, len(cycleSet))
	for member := range cycleSet {
		cycles = append(cycles, member)
	}
	sort.Strings(cycles)
	return cycles
}

func (c *Collector) collectPermissions() {
	if c.contract.Permissions == nil {
		return
	}
	p := c.contract.Permissions
	report := &PermissionReport{
		TargetTypeCounts:  make(map[string]int),
		ScopeCounts:       make(map[string]int),
		ActionCounts:      make(map[string]int),
		InheritanceCounts: make(map[string]int),
	}
	targetCounts := make(map[string]int)
	agents := make(map[string]record)
	for _, agent := range c.records[p.AgentTable] {
		agents[scalarString(agent.values[p.AgentIDField])] = agent
	}
	members := make(map[string]record)
	for _, member := range c.records[p.MemberTable] {
		members[scalarString(member.values[p.MemberIDField])] = member
	}
	allowedTargets := make(map[string]struct{}, len(p.AllowedTargets))
	for _, target := range p.AllowedTargets {
		allowedTargets[target] = struct{}{}
	}
	for _, target := range c.records[p.TargetTable] {
		targetType := scalarString(target.values[p.TargetTypeField])
		agentID := scalarString(target.values[p.TargetAgentField])
		targetID := scalarString(target.values[p.TargetIDField])
		targetCounts[agentID]++
		report.ScopeCounts[c.classifiedEnum(p.TargetTable, p.ScopeField, scalarString(target.values[p.ScopeField]))]++
		report.ActionCounts[c.classifiedEnum(p.TargetTable, p.ActionField, scalarString(target.values[p.ActionField]))]++
		report.InheritanceCounts[c.classifiedEnum(p.TargetTable, p.InheritanceField, scalarString(target.values[p.InheritanceField]))]++
		if _, ok := allowedTargets[targetType]; !ok {
			report.TargetTypeCounts["__unknown__"]++
			c.reject("permissions", p.TargetTable, target.anonymousID, "PERMISSION_UNPROVEN", "fatal", "downgrade_private", nil, false, []byte(targetType))
		} else {
			report.TargetTypeCounts[targetType]++
		}
		agent, agentOK := agents[agentID]
		validTarget := agentOK
		if validTarget {
			agentWorkspace := scalarString(agent.values[p.AgentWorkspaceField])
			switch targetType {
			case p.WorkspaceTargetType:
				validTarget = targetID == agentWorkspace
			case p.MemberTargetType:
				member, ok := members[targetID]
				validTarget = ok && scalarString(member.values[p.MemberWorkspaceField]) == agentWorkspace
			default:
				validTarget = false
			}
		}
		if !validTarget {
			report.InvalidTargetCount++
			c.reject("permissions", p.TargetTable, target.anonymousID, "PERMISSION_UNPROVEN", "fatal", "downgrade_private", nil, false, []byte("invalid-target:"+targetType+":"+targetID))
		}
	}
	for _, agent := range c.records[p.AgentTable] {
		mode := scalarString(agent.values[p.ModeField])
		count := targetCounts[scalarString(agent.values[p.AgentIDField])]
		switch mode {
		case p.PrivateMode:
			report.PrivateCount++
			if count > 0 {
				report.PrivateWithTargetCount++
				c.reject("permissions", p.AgentTable, agent.anonymousID, "PERMISSION_UNPROVEN", "fatal", "downgrade_private", nil, false, []byte("private-with-target"))
			}
		case p.PublicMode:
			report.PublicToCount++
			if count == 0 {
				report.PublicWithoutTargetCount++
				c.reject("permissions", p.AgentTable, agent.anonymousID, "PERMISSION_UNPROVEN", "fatal", "downgrade_private", nil, false, []byte("public-without-target"))
			}
		default:
			c.reject("permissions", p.AgentTable, agent.anonymousID, "PERMISSION_UNPROVEN", "fatal", "downgrade_private", nil, false, []byte(mode))
		}
	}
	c.report.Permissions = report
}

func (c *Collector) collectAttachments() error {
	if c.contract.Attachments == nil {
		return nil
	}
	a := c.contract.Attachments
	report := &AttachmentReport{StorageTypeCounts: make(map[string]int)}
	root, err := filepath.Abs(c.attachmentsDir)
	if err != nil {
		return fmt.Errorf("resolve attachments root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve attachments root symlinks: %w", err)
	}
	for _, rec := range c.records[a.Table] {
		report.RowsChecked++
		storageType := scalarString(rec.values[a.StorageTypeField])
		report.StorageTypeCounts[c.classifiedEnum(a.Table, a.StorageTypeField, storageType)]++
		rel := scalarString(rec.values[a.StorageKeyField])
		path := filepath.Join(root, filepath.FromSlash(rel))
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			resolved, err = filepath.Abs(resolved)
		}
		if err == nil && resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			c.reject("attachments", a.Table, rec.anonymousID, "ATTACHMENT_PATH_ESCAPE", "fatal", "reject", nil, false, []byte(rel))
			continue
		}
		if err != nil {
			report.MissingCount++
			c.reject("attachments", a.Table, rec.anonymousID, "ATTACHMENT_OBJECT_MISSING", "fatal", "reject", nil, true, []byte(rel))
			continue
		}
		file, err := os.Open(resolved)
		if err != nil {
			report.MissingCount++
			c.reject("attachments", a.Table, rec.anonymousID, "ATTACHMENT_OBJECT_MISSING", "fatal", "reject", nil, true, []byte(rel))
			continue
		}
		hash := sha256.New()
		size, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf("hash attachment object: %v / %v", copyErr, closeErr)
		}
		report.TotalBytes += size
		wantSize, err := strconv.ParseInt(scalarString(rec.values[a.SizeField]), 10, 64)
		if err != nil || wantSize != size {
			report.SizeMismatchCount++
			c.reject("attachments", a.Table, rec.anonymousID, "ATTACHMENT_SIZE_MISMATCH", "fatal", "reject", nil, true, hash.Sum(nil))
		}
		wantHash := strings.ToLower(scalarString(rec.values[a.SHA256Field]))
		gotHash := hex.EncodeToString(hash.Sum(nil))
		if wantHash != gotHash {
			report.HashMismatchCount++
			c.reject("attachments", a.Table, rec.anonymousID, "ATTACHMENT_HASH_MISMATCH", "fatal", "reject", nil, true, hash.Sum(nil))
		}
	}
	c.report.Attachments = report
	return nil
}

func (c *Collector) collectUsage() {
	if c.contract.Usage == nil {
		return
	}
	u := c.contract.Usage
	report := &UsageReport{UnitFields: u.UnitFields, Totals: make(map[string]string)}
	totals := make(map[string]int64)
	for _, rec := range c.records[u.Table] {
		report.RowsChecked++
		if rec.values[u.TaskField] == nil || scalarString(rec.values[u.TaskField]) == "" {
			c.reject("usage", u.Table, rec.anonymousID, "USAGE_SEMANTICS_UNKNOWN", "fatal", "reject", nil, false, []byte("missing-task"))
		}
		for field := range u.UnitFields {
			value, err := strconv.ParseInt(scalarString(rec.values[field]), 10, 64)
			if err != nil || value < 0 {
				c.reject("usage", u.Table, rec.anonymousID, "USAGE_SEMANTICS_UNKNOWN", "fatal", "reject", nil, false, []byte(field))
				continue
			}
			totals[field] += value
		}
	}
	for field, total := range totals {
		report.Totals[field] = strconv.FormatInt(total, 10)
	}
	c.report.Usage = report
}

func (c *Collector) collectTasks() {
	if c.contract.Tasks == nil {
		return
	}
	t := c.contract.Tasks
	report := &TaskReport{StatusCounts: make(map[string]int)}
	terminal := make(map[string]struct{}, len(t.TerminalStatuses))
	for _, status := range t.TerminalStatuses {
		terminal[status] = struct{}{}
	}
	for _, rec := range c.records[t.Table] {
		report.RowsChecked++
		status := scalarString(rec.values[t.StatusField])
		report.StatusCounts[c.classifiedEnum(t.Table, t.StatusField, status)]++
		if _, ok := terminal[status]; !ok {
			report.NonterminalCount++
			c.reject("tasks", t.Table, rec.anonymousID, "NONTERMINAL_TASK", "fatal", "stop_snapshot", nil, true, []byte(status))
		}
		if t.OriginatorField != "" && rec.values[t.OriginatorField] != nil && scalarString(rec.values[t.OriginatorField]) != scalarString(rec.values[t.AccountableField]) {
			report.AttributionInvalidCount++
			c.reject("tasks", t.Table, rec.anonymousID, "ATTRIBUTION_INVALID", "fatal", "reject", nil, false, []byte("originator-accountable"))
		}
	}
	c.report.Tasks = report
}

func (c *Collector) classifiedEnum(tableName, fieldName, value string) string {
	for i := range c.contract.Tables {
		if c.contract.Tables[i].Name != tableName {
			continue
		}
		for _, field := range c.contract.Tables[i].Fields {
			if field.Name != fieldName {
				continue
			}
			for _, allowed := range field.Enum {
				if value == allowed {
					return value
				}
			}
		}
	}
	return "__unknown__"
}

func (c *Collector) reject(domain, entityType, anonymousID, reason, severity, action string, dependencies []string, retryable bool, evidence []byte) {
	evidenceMAC := hmac.New(sha256.New, c.key)
	_, _ = evidenceMAC.Write(evidence)
	c.report.Rejections = append(c.report.Rejections, Rejection{
		MappingVersion: MappingVersion,
		SnapshotIDHash: c.report.SnapshotIDHash,
		Domain:         domain,
		EntityType:     entityType,
		AnonymousID:    anonymousID,
		ReasonCode:     reason,
		Severity:       severity,
		PlannedAction:  action,
		DependencyIDs:  append([]string(nil), dependencies...),
		BatchID:        "collector-preflight",
		Retryable:      retryable,
		EvidenceHash:   hex.EncodeToString(evidenceMAC.Sum(nil)),
	})
}
