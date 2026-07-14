package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/authority"
	"github.com/multica-ai/multica/server/internal/cli"
)

var authorityCmd = &cobra.Command{
	Use:   "authority",
	Short: "Verify a pinned Multica authority",
}

var authorityPinCmd = &cobra.Command{
	Use:   "pin",
	Short: "Store an explicit authority pin for this profile",
	RunE:  runAuthorityPin,
}

var authorityVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the pinned authority for this server",
	RunE:  runAuthorityVerify,
}

func init() {
	authorityCmd.AddCommand(authorityPinCmd)
	authorityCmd.AddCommand(authorityVerifyCmd)

	authorityPinCmd.Flags().String("server-url", "", "Expected server URL")
	authorityPinCmd.Flags().String("authority-id", "", "Expected authority ID")
	authorityPinCmd.Flags().String("public-key", "", "Expected Ed25519 public key as unpadded base64url")
	authorityPinCmd.Flags().String("db-system-identifier", "", "Expected PostgreSQL system_identifier decimal text")
	authorityPinCmd.Flags().Int64("db-oid", 0, "Expected PostgreSQL database OID")
	authorityPinCmd.Flags().String("db-name", "", "Expected PostgreSQL database name")

	authorityVerifyCmd.Flags().String("output", "json", "Output format: json or table")
}

func runAuthorityPin(cmd *cobra.Command, _ []string) error {
	serverURL, _ := cmd.Flags().GetString("server-url")
	authorityID, _ := cmd.Flags().GetString("authority-id")
	publicKey, _ := cmd.Flags().GetString("public-key")
	systemID, _ := cmd.Flags().GetString("db-system-identifier")
	dbOID, _ := cmd.Flags().GetInt64("db-oid")
	dbName, _ := cmd.Flags().GetString("db-name")
	if serverURL == "" {
		return fmt.Errorf("--server-url is required")
	}
	if authorityID == "" {
		return fmt.Errorf("--authority-id is required")
	}
	if publicKey == "" {
		return fmt.Errorf("--public-key is required")
	}
	if systemID == "" {
		return fmt.Errorf("--db-system-identifier is required")
	}
	if dbOID <= 0 {
		return fmt.Errorf("--db-oid is required")
	}
	if dbName == "" {
		return fmt.Errorf("--db-name is required")
	}
	normalized, err := authority.NormalizeServerURL(serverURL)
	if err != nil {
		return err
	}
	if _, err := authority.DecodePublicKey(publicKey); err != nil {
		return err
	}
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	cfg.AuthorityPin = &authority.Pin{
		ServerURL:   normalized,
		PublicKey:   publicKey,
		AuthorityID: authorityID,
		DBIdentity: authority.DBIdentity{
			SystemIdentifier: systemID,
			DatabaseOID:      dbOID,
			DatabaseName:     dbName,
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Authority pin stored")
	return nil
}

func runAuthorityVerify(cmd *cobra.Command, _ []string) error {
	res, err := verifyAuthorityForCommand(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"AUTHORITY", "DATABASE", "COMMIT"}, [][]string{{
			res.AuthorityID,
			res.DBIdentity.DatabaseName,
			res.ServerCommit,
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, res)
}

func verifyAuthorityForCommand(cmd *cobra.Command) (authority.Attestation, error) {
	serverURL := resolveServerURL(cmd)
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return authority.Attestation{}, err
	}
	if cfg.AuthorityPin == nil {
		return authority.Attestation{}, fmt.Errorf("authority pin is not configured; run multica authority pin with operator-provided values")
	}
	client := cli.NewAPIClient(serverURL, "", "")
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	return verifyAuthorityWithClient(ctx, client, *cfg.AuthorityPin, time.Now)
}

func verifyAuthorityWithClient(ctx context.Context, client *cli.APIClient, pin authority.Pin, now func() time.Time) (authority.Attestation, error) {
	nonce, err := authority.GenerateNonce(nil)
	if err != nil {
		return authority.Attestation{}, err
	}
	var att authority.Attestation
	if err := client.PostJSONStrict(ctx, "/api/authority/attest", map[string]string{"nonce": nonce}, &att, 16*1024); err != nil {
		return authority.Attestation{}, fmt.Errorf("authority attest: %w", err)
	}
	if att.Nonce != nonce {
		return authority.Attestation{}, fmt.Errorf("authority nonce mismatch")
	}
	if now == nil {
		now = time.Now
	}
	if err := authority.Verify(att, pin, client.BaseURL, now(), 2*time.Minute, 30*time.Second); err != nil {
		return authority.Attestation{}, err
	}
	return att, nil
}
