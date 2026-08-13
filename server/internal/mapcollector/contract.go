package mapcollector

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var allowedFieldRoles = map[string]struct{}{
	"": {}, "stable_id": {}, "workspace_ref": {}, "owner_ref": {},
	"parent_ref": {}, "task_ref": {}, "ordering": {}, "enum": {},
	"time": {}, "tombstone": {}, "storage_key": {}, "storage_type": {},
	"size": {}, "byte_hash": {}, "usage": {}, "cost": {},
}

func LoadContract(path string) (*Contract, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read contract: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var contract Contract
	if err := dec.Decode(&contract); err != nil {
		return nil, nil, fmt.Errorf("decode contract: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("decode contract: trailing JSON value")
		}
		return nil, nil, fmt.Errorf("decode contract trailing data: %w", err)
	}
	if err := contract.Validate(); err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(contract)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize contract: %w", err)
	}
	canonical, err = jsoncanonicalizer.Transform(canonical)
	if err != nil {
		return nil, nil, fmt.Errorf("RFC 8785 canonicalize contract: %w", err)
	}
	return &contract, canonical, nil
}

func (c *Contract) Validate() error {
	if c.MappingVersion != MappingVersion {
		return fmt.Errorf("mapping_version must be %q", MappingVersion)
	}
	if c.SnapshotLabel == "" {
		return fmt.Errorf("snapshot_label is required")
	}
	if c.PostgresVersion != "17.10" {
		return fmt.Errorf("postgres_version must be 17.10")
	}
	if err := validIdentifier("schema", c.Schema); err != nil {
		return err
	}
	if len(c.Tables) == 0 {
		return fmt.Errorf("at least one table is required")
	}
	tables := make(map[string]*TableContract, len(c.Tables))
	for i := range c.Tables {
		t := &c.Tables[i]
		if err := validIdentifier("table", t.Name); err != nil {
			return err
		}
		if t.Domain == "" || t.IDField == "" || len(t.Fields) == 0 {
			return fmt.Errorf("table %s requires domain, id_field, and fields", t.Name)
		}
		if _, exists := tables[t.Name]; exists {
			return fmt.Errorf("duplicate table %s", t.Name)
		}
		tables[t.Name] = t
		fields := make(map[string]struct{}, len(t.Fields))
		for _, f := range t.Fields {
			if err := validIdentifier("field", f.Name); err != nil {
				return err
			}
			if f.Type == "" {
				return fmt.Errorf("field %s.%s requires type", t.Name, f.Name)
			}
			if _, ok := allowedFieldRoles[f.Role]; !ok {
				return fmt.Errorf("field %s.%s has unknown role %q", t.Name, f.Name, f.Role)
			}
			if _, exists := fields[f.Name]; exists {
				return fmt.Errorf("duplicate field %s.%s", t.Name, f.Name)
			}
			fields[f.Name] = struct{}{}
			if len(f.Enum) > 0 {
				if err := uniqueStrings("enum "+t.Name+"."+f.Name, f.Enum); err != nil {
					return err
				}
			}
		}
		if _, ok := fields[t.IDField]; !ok {
			return fmt.Errorf("table %s id_field %s is not allowlisted", t.Name, t.IDField)
		}
		for _, denied := range t.DeniedColumns {
			if err := validIdentifier("denied column", denied); err != nil {
				return err
			}
			if _, ok := fields[denied]; ok {
				return fmt.Errorf("field %s.%s cannot be both allowed and denied", t.Name, denied)
			}
		}
		if err := uniqueStrings("denied columns for "+t.Name, t.DeniedColumns); err != nil {
			return err
		}
		for _, unique := range t.Unique {
			if unique.Name == "" || len(unique.Fields) == 0 {
				return fmt.Errorf("table %s has invalid unique contract", t.Name)
			}
			if err := requireFields(t, unique.Fields); err != nil {
				return fmt.Errorf("unique %s: %w", unique.Name, err)
			}
		}
	}
	for _, ref := range c.References {
		if ref.Name == "" || ref.Domain == "" || len(ref.FromFields) == 0 || len(ref.FromFields) != len(ref.ToFields) {
			return fmt.Errorf("reference %q has invalid name/domain/field arity", ref.Name)
		}
		from, fromOK := tables[ref.FromTable]
		to, toOK := tables[ref.ToTable]
		if !fromOK || !toOK {
			return fmt.Errorf("reference %s names unknown table", ref.Name)
		}
		if err := requireFields(from, ref.FromFields); err != nil {
			return fmt.Errorf("reference %s: %w", ref.Name, err)
		}
		if err := requireFields(to, ref.ToFields); err != nil {
			return fmt.Errorf("reference %s: %w", ref.Name, err)
		}
		if (ref.FromWorkspaceField == "") != (ref.ToWorkspaceField == "") {
			return fmt.Errorf("reference %s must specify both workspace fields", ref.Name)
		}
		if ref.FromWorkspaceField != "" {
			if err := requireFields(from, []string{ref.FromWorkspaceField}); err != nil {
				return fmt.Errorf("reference %s: %w", ref.Name, err)
			}
			if err := requireFields(to, []string{ref.ToWorkspaceField}); err != nil {
				return fmt.Errorf("reference %s: %w", ref.Name, err)
			}
		}
	}
	if c.Owners != nil {
		o := c.Owners
		if tables[o.WorkspaceTable] == nil || tables[o.MemberTable] == nil || o.OwnerRole == "" {
			return fmt.Errorf("owners names unknown table or empty owner_role")
		}
		if err := requireFields(tables[o.WorkspaceTable], []string{o.WorkspaceIDField}); err != nil {
			return err
		}
		if err := requireFields(tables[o.MemberTable], []string{o.MemberWorkspaceField, o.MemberRoleField}); err != nil {
			return err
		}
	}
	if c.Permissions != nil {
		p := c.Permissions
		if tables[p.AgentTable] == nil || tables[p.TargetTable] == nil || tables[p.MemberTable] == nil {
			return fmt.Errorf("permissions names unknown table")
		}
		if err := requireFields(tables[p.AgentTable], []string{p.AgentIDField, p.AgentWorkspaceField, p.ModeField}); err != nil {
			return err
		}
		if err := requireFields(tables[p.TargetTable], []string{p.TargetAgentField, p.TargetTypeField, p.TargetIDField, p.ScopeField, p.ActionField, p.InheritanceField}); err != nil {
			return err
		}
		if err := requireFields(tables[p.MemberTable], []string{p.MemberIDField, p.MemberWorkspaceField}); err != nil {
			return err
		}
		if p.PrivateMode == "" || p.PublicMode == "" || p.PrivateMode == p.PublicMode || len(p.AllowedTargets) == 0 || p.WorkspaceTargetType == "" || p.MemberTargetType == "" {
			return fmt.Errorf("permissions modes and allowed_targets are required")
		}
	}
	if c.Attachments != nil {
		a := c.Attachments
		if tables[a.Table] == nil {
			return fmt.Errorf("attachments names unknown table")
		}
		if err := requireFields(tables[a.Table], []string{a.StorageKeyField, a.StorageTypeField, a.SizeField, a.SHA256Field}); err != nil {
			return err
		}
	}
	if c.Usage != nil {
		u := c.Usage
		if tables[u.Table] == nil || len(u.UnitFields) == 0 {
			return fmt.Errorf("usage table and unit_fields are required")
		}
		fields := []string{u.TaskField}
		for field, unit := range u.UnitFields {
			if unit == "" {
				return fmt.Errorf("usage field %s requires a unit", field)
			}
			fields = append(fields, field)
		}
		if err := requireFields(tables[u.Table], fields); err != nil {
			return err
		}
	}
	if c.Tasks != nil {
		t := c.Tasks
		if tables[t.Table] == nil || len(t.TerminalStatuses) == 0 {
			return fmt.Errorf("tasks table and terminal_statuses are required")
		}
		if err := requireFields(tables[t.Table], []string{t.StatusField}); err != nil {
			return err
		}
		if (t.OriginatorField == "") != (t.AccountableField == "") {
			return fmt.Errorf("tasks must specify both attribution fields")
		}
		if t.OriginatorField != "" {
			if err := requireFields(tables[t.Table], []string{t.OriginatorField, t.AccountableField}); err != nil {
				return err
			}
		}
	}
	return nil
}

func validIdentifier(kind, value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("invalid %s identifier %q", kind, value)
	}
	return nil
}

func requireFields(table *TableContract, required []string) error {
	fields := make(map[string]struct{}, len(table.Fields))
	for _, f := range table.Fields {
		fields[f.Name] = struct{}{}
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("field %s.%s is not allowlisted", table.Name, name)
		}
	}
	return nil
}

func uniqueStrings(name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains duplicate %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func contractHash(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func sortedCopy(values []string) []string {
	copyOf := append([]string(nil), values...)
	sort.Strings(copyOf)
	return copyOf
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
