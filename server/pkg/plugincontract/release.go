package plugincontract

import (
	"fmt"
	"strings"
)

const (
	SourceBundled    = "bundled"
	SourcePrivateDev = "private_dev"
)

type ReleaseCandidate struct {
	Archive        []byte
	SourceKind     string
	SourceRef      string
	SignatureKeyID string
	Signature      []byte
}

type ValidatedRelease struct {
	Manifest          Manifest
	CanonicalManifest []byte
	ManifestDigest    string
	ArchiveDigest     string
	ArtifactDigest    string
	ArtifactSize      int64
	SourceKind        string
	SourceRef         string
	SignatureKeyID    string
	Signature         []byte
	Contributions     []ValidatedContribution
	Files             []ArtifactFile
}

type ValidatedContribution struct {
	Key                    string
	Type                   string
	SchemaVersion          int32
	DisplayName            string
	Description            string
	EntryPath              string
	EntryDigest            string
	ArtifactDigest         string
	RequiredDaemonFeatures []string
	Ordinal                int32
}

// ValidateReleaseCandidate is the publication boundary. Bundled releases must
// carry a signature from the configured release trust store; private-dev
// releases may be unsigned, but a supplied signature is always verified.
func ValidateReleaseCandidate(candidate ReleaseCandidate, verifier ReleaseVerifier) (ValidatedRelease, error) {
	if candidate.SourceKind != SourceBundled && candidate.SourceKind != SourcePrivateDev {
		return ValidatedRelease{}, fmt.Errorf("plugin release source_kind %q is unsupported", candidate.SourceKind)
	}
	if candidate.SourceRef == "" || candidate.SourceRef != strings.TrimSpace(candidate.SourceRef) || len(candidate.SourceRef) > 2048 {
		return ValidatedRelease{}, fmt.Errorf("plugin release source_ref is invalid")
	}
	if (candidate.SignatureKeyID == "") != (len(candidate.Signature) == 0) {
		return ValidatedRelease{}, fmt.Errorf("plugin release signature and key id must be supplied together")
	}
	if candidate.SourceKind == SourceBundled && candidate.SignatureKeyID == "" {
		return ValidatedRelease{}, fmt.Errorf("bundled plugin release must be signed")
	}

	artifact, err := ValidateArtifact(candidate.Archive)
	if err != nil {
		return ValidatedRelease{}, err
	}
	manifestDigest := DigestBytes(artifact.CanonicalManifest)
	envelope := ReleaseEnvelope{
		PluginKey:      artifact.Manifest.Metadata.Key,
		Version:        artifact.Manifest.Metadata.Version,
		ManifestDigest: manifestDigest,
		ArchiveDigest:  artifact.ArchiveDigest,
		ArtifactDigest: artifact.ArtifactDigest,
	}
	if candidate.SignatureKeyID != "" {
		if verifier == nil {
			return ValidatedRelease{}, fmt.Errorf("plugin release signature verifier is not configured")
		}
		if err := verifier.VerifyRelease(candidate.SignatureKeyID, envelope, candidate.Signature); err != nil {
			return ValidatedRelease{}, err
		}
	}

	files := make(map[string]ArtifactFile, len(artifact.Files))
	for _, file := range artifact.Files {
		files[file.Path] = file
	}
	contributions := make([]ValidatedContribution, 0, len(artifact.Manifest.Contributes.AgentSkills))
	for ordinal, skill := range artifact.Manifest.Contributes.AgentSkills {
		entry := files[skill.Entry]
		contributions = append(contributions, ValidatedContribution{
			Key:                    skill.Key,
			Type:                   ContributionAgentSkillV1,
			SchemaVersion:          1,
			DisplayName:            skill.Name,
			Description:            skill.Description,
			EntryPath:              skill.Entry,
			EntryDigest:            entry.Digest,
			ArtifactDigest:         artifact.ArtifactDigest,
			RequiredDaemonFeatures: append([]string(nil), artifact.Manifest.Compatibility.RequiredDaemonFeatures...),
			Ordinal:                int32(ordinal),
		})
	}

	return ValidatedRelease{
		Manifest:          artifact.Manifest,
		CanonicalManifest: append([]byte(nil), artifact.CanonicalManifest...),
		ManifestDigest:    manifestDigest,
		ArchiveDigest:     artifact.ArchiveDigest,
		ArtifactDigest:    artifact.ArtifactDigest,
		ArtifactSize:      artifact.SizeBytes,
		SourceKind:        candidate.SourceKind,
		SourceRef:         candidate.SourceRef,
		SignatureKeyID:    candidate.SignatureKeyID,
		Signature:         append([]byte(nil), candidate.Signature...),
		Contributions:     contributions,
		Files:             append([]ArtifactFile(nil), artifact.Files...),
	}, nil
}
