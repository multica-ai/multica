// Package plugintest contains reference releases used only by Plugin V1 tests.
package plugintest

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const (
	ReviewReadinessPluginKey = "ai.multica.software-delivery"
	ReviewReadinessVersion   = "1.0.0"
	releaseKeyID             = "multica-plugin-release-test-2026-01"
	releasePublicKeyBase64   = "l5+eyMIoTHPAC3AWhgpHgxEvjwYtt1gsj1PvQB8zwvU="
	releaseSignatureBase64   = "sLa3lBwIrqg1zbKcK6b53HRE/8ZLXG+hBeP2aq/Cn0S/36mPIaMUkiSL+3N+YL/a04YkL26sQd9CpjCgx7zqBQ=="
)

const reviewReadinessSkill = `---
name: review-readiness
description: Verify that a software change has enough evidence, rollout planning, and review context to be safely reviewed.
---

# Review readiness

Use this skill when preparing a software change for review or deciding whether it is ready to hand off.

## Check the change

1. State the user-visible or operational outcome in one sentence.
2. Confirm the diff is scoped to that outcome and call out intentional exclusions.
3. Record the tests or other evidence that directly exercise the changed behavior.
4. Identify data migrations, compatibility constraints, rollout steps, and rollback steps.
5. Surface unresolved risks or decisions instead of hiding them in implementation detail.

## Produce the handoff

Return a compact review brief with:

- outcome and scope;
- key architectural choices;
- evidence run and its result;
- rollout and rollback notes;
- open risks or decisions;
- exact files or interfaces where reviewers should focus.

Do not claim readiness when required evidence is missing. Say what is missing and the smallest next action that would provide it.
`

func reviewReadinessManifest() plugincontract.Manifest {
	return plugincontract.Manifest{
		APIVersion: plugincontract.APIVersionV1,
		Kind:       plugincontract.KindPlugin,
		Metadata: plugincontract.Metadata{
			Key:         ReviewReadinessPluginKey,
			Name:        "Software Delivery",
			Description: "Official Multica software-delivery workflow skills.",
			Version:     ReviewReadinessVersion,
			Publisher:   "multica",
		},
		Compatibility: plugincontract.Compatibility{
			HostAPI: ">=1.0.0 <2.0.0",
			RequiredDaemonFeatures: []string{
				plugincontract.DaemonFeatureExecutionManifestV1,
				plugincontract.DaemonFeatureAgentSkillV1,
			},
		},
		RequestedCapabilities: []string{plugincontract.CapabilityAgentSkillContribute},
		Contributes: plugincontract.ManifestContributions{AgentSkills: []plugincontract.AgentSkillContribution{{
			Key:         "review-readiness",
			Name:        "review-readiness",
			Description: "Verify that a software change is ready for review and handoff.",
			Entry:       "skills/review-readiness/SKILL.md",
		}}},
	}
}

func reviewReadinessArchive() ([]byte, error) {
	manifest, err := json.Marshal(reviewReadinessManifest())
	if err != nil {
		return nil, err
	}
	files := []struct {
		name    string
		content []byte
	}{
		{name: plugincontract.ManifestFilename, content: manifest},
		{name: "skills/review-readiness/SKILL.md", content: []byte(reviewReadinessSkill)},
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Store}
		header.SetMode(0o600)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create reference plugin entry: %w", err)
		}
		if _, err := entry.Write(file.content); err != nil {
			return nil, fmt.Errorf("write reference plugin entry: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close reference plugin archive: %w", err)
	}
	return buffer.Bytes(), nil
}

// ReviewReadinessRelease returns a signed reference release for lifecycle and
// execution-chain tests. It is not imported by the production server.
func ReviewReadinessRelease() (plugincontract.ValidatedRelease, error) {
	archive, err := reviewReadinessArchive()
	if err != nil {
		return plugincontract.ValidatedRelease{}, err
	}
	publicKey, err := base64.StdEncoding.DecodeString(releasePublicKeyBase64)
	if err != nil {
		return plugincontract.ValidatedRelease{}, fmt.Errorf("decode reference plugin trust root: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(releaseSignatureBase64)
	if err != nil {
		return plugincontract.ValidatedRelease{}, fmt.Errorf("decode reference plugin signature: %w", err)
	}
	trustStore, err := plugincontract.NewEd25519TrustStore(map[string]ed25519.PublicKey{
		releaseKeyID: ed25519.PublicKey(publicKey),
	})
	if err != nil {
		return plugincontract.ValidatedRelease{}, err
	}
	return plugincontract.ValidateReleaseCandidate(plugincontract.ReleaseCandidate{
		Archive:        archive,
		SourceKind:     plugincontract.SourceBundled,
		SourceRef:      "test://software-delivery/1.0.0",
		SignatureKeyID: releaseKeyID,
		Signature:      signature,
	}, trustStore)
}
