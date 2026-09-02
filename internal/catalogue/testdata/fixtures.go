// Package testdata supplies catalogue entries covering the four kinds that
// selection has to tell apart. Each kind encodes a defect found during research
// that the implementation must not reintroduce:
//
//   - CommercialSafe vs NonCommercial — usage terms gate offers, so a model
//     whose licence forbids the declared usage is withheld with the restricting
//     term named (D4, FR-054/FR-055).
//   - StreamingRosterMember vs StreamingIneligibleMoE — the streaming runtime
//     supports a closed, NAMED list of families. Both fixtures are
//     mixture-of-experts; only one is on the roster. Any implementation that
//     infers eligibility from architecture will treat them the same, and that is
//     exactly the bug (D1).
//
// Model names, sizes and licences are drawn from real models so the fixtures
// behave like the catalogue they stand in for; the digests are illustrative
// fixture values, not hashes of real weight files.
package testdata

import "github.com/HelixDevelopment/HelixLLM/internal/catalogue"

const (
	mib = uint64(1) << 20
	gib = uint64(1) << 30
)

// StreamingFamilyDeepSeekR1 is the name the streaming runtime uses for the
// DeepSeek-R1 family in its declared supported set.
const StreamingFamilyDeepSeekR1 = "deepseek-r1"

// StreamingFamilyQwen3MoE is the name that would identify the Qwen3-30B-A3B
// family. It is deliberately NOT in the roster below: the model is
// mixture-of-experts with no streaming support path.
const StreamingFamilyQwen3MoE = "qwen3-moe"

// StreamingRoster is the streaming runtime's declared supported set, as data.
// It admits StreamingFamilyDeepSeekR1 and nothing else, so a lookup of
// StreamingFamilyQwen3MoE misses despite that family being MoE.
func StreamingRoster() catalogue.Roster {
	return catalogue.NewRoster(StreamingFamilyDeepSeekR1)
}

// CommercialSafeEntry is an Apache-2.0 text model: it permits commercial,
// personal and research use and carries no exclusionary restriction, so the
// usage-terms filter never withholds it.
func CommercialSafeEntry() catalogue.Entry {
	return catalogue.Entry{
		ModelID:      "qwen2.5-coder-7b-instruct",
		Variant:      "q4_k_m",
		Family:       catalogue.FamilyText,
		Architecture: catalogue.ArchitectureDense,
		Descriptor: catalogue.Descriptor{
			ParameterCount:  7_620_000_000,
			Quantisation:    "q4_k_m",
			Specialisations: []string{"code", "instruction-following"},
		},
		MemoryRequiredBytes:  6 * gib,
		StorageRequiredBytes: 4*gib + 700*mib,
		RequiresAccelerator:  false,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "Apache-2.0",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsageCommercial,
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
		},
		Runtime: catalogue.RuntimeInMemory,
		// Not on the streaming roster, and it does not need to be: it fits in
		// memory, so the in-memory runtime serves it (D6).
		StreamingRoster: catalogue.RosterMembership{},
		ExpectedCapability: catalogue.ExpectedCapability{
			ContextTokens:             32768,
			ThroughputTokensPerSecond: 22.5,
			Modalities:                []string{"text"},
		},
		Integrity: catalogue.IntegrityExpectation{
			Algorithm: catalogue.DigestSHA256,
			Digest:    "3f1c0a9d7b52e84c6f0d1a3b8e57c2049da6b13f7e08c5a24bd93f6017ce48ab",
			SizeBytes: 4*gib + 700*mib,
		},
	}
}

// NonCommercialEntry is a text-to-speech model under a licence that forbids
// commercial use and separately requires attribution.
//
// The two restrictions are both present on purpose: a caller withholding this
// entry for a commercial declared usage must name the NON-COMMERCIAL term, not
// merely the first restriction it encounters. Attribution constrains how output
// is used but withholds nothing.
func NonCommercialEntry() catalogue.Entry {
	return catalogue.Entry{
		ModelID:      "xtts-v2",
		Family:       catalogue.FamilyTextToSpeech,
		Architecture: catalogue.ArchitectureEncoderDecoder,
		Descriptor: catalogue.Descriptor{
			ParameterCount:  467_000_000,
			Specialisations: []string{"voice-cloning", "multilingual"},
		},
		MemoryRequiredBytes:  3 * gib,
		StorageRequiredBytes: 1*gib + 872*mib,
		RequiresAccelerator:  false,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "coqui-public-model-license-1.0",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
			Restrictions: []catalogue.Restriction{
				{
					Term:      catalogue.TermAttributionRequired,
					Reference: "cpml-1.0 §3",
				},
				{
					Term:      catalogue.TermNonCommercial,
					Excludes:  []catalogue.UsagePurpose{catalogue.UsageCommercial},
					Reference: "cpml-1.0 §2(a)",
				},
			},
		},
		Runtime:         catalogue.RuntimeInMemory,
		StreamingRoster: catalogue.RosterMembership{},
		ExpectedCapability: catalogue.ExpectedCapability{
			Modalities: []string{"text"},
		},
		Integrity: catalogue.IntegrityExpectation{
			Algorithm: catalogue.DigestSHA256,
			Digest:    "b70e2f45c81a936d0427ef5c1938ba60d4e7c25918af03bd6e1c47a802f95d13",
			SizeBytes: 1*gib + 872*mib,
		},
	}
}

// RevenueCappedEntry is a second usage-terms case: commercial use is granted,
// but only below a revenue ceiling the licence names. It exists so a caller can
// be shown to read the threshold rather than collapsing every licence into
// permitted/forbidden.
func RevenueCappedEntry() catalogue.Entry {
	return catalogue.Entry{
		ModelID:      "flux.1-dev",
		Family:       catalogue.FamilyImageGeneration,
		Architecture: catalogue.ArchitectureDiffusion,
		Descriptor: catalogue.Descriptor{
			ParameterCount:  12_000_000_000,
			Specialisations: []string{"text-to-image"},
		},
		MemoryRequiredBytes:  24 * gib,
		StorageRequiredBytes: 23*gib + 800*mib,
		RequiresAccelerator:  true,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "flux-1-dev-non-commercial-1.0",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
			Restrictions: []catalogue.Restriction{
				{
					Term:     catalogue.TermRevenueCap,
					Excludes: []catalogue.UsagePurpose{catalogue.UsageCommercial},
					Threshold: catalogue.Amount{
						Value:    1_000_000,
						Currency: "USD",
						Period:   "annual",
					},
					Reference: "flux-1-dev-nc-1.0 §2(d)",
				},
			},
		},
		Runtime:         catalogue.RuntimeInMemory,
		StreamingRoster: catalogue.RosterMembership{},
		ExpectedCapability: catalogue.ExpectedCapability{
			Modalities: []string{"text"},
		},
		Integrity: catalogue.IntegrityExpectation{
			Algorithm: catalogue.DigestSHA256,
			Digest:    "6d29e0b7148c3af5029e7bd41c60835ae9f2c74d0b31e856af7920cd41e6b7f2",
			SizeBytes: 23*gib + 800*mib,
		},
	}
}

// StreamingRosterMemberEntry is a mixture-of-experts model that IS on the
// streaming runtime's roster.
//
// Its on-disk footprint is roughly eighteen times its memory requirement, which
// is the whole point of the streaming path and the reason storage headroom is an
// axis independent of memory headroom (D2).
func StreamingRosterMemberEntry() catalogue.Entry {
	return catalogue.Entry{
		ModelID:      "deepseek-r1-671b",
		Variant:      "q4_k_m",
		Family:       catalogue.FamilyText,
		Architecture: catalogue.ArchitectureMixtureOfExperts,
		Descriptor: catalogue.Descriptor{
			ParameterCount:   671_000_000_000,
			ActiveParameters: 37_000_000_000,
			Quantisation:     "q4_k_m",
			Specialisations:  []string{"reasoning"},
		},
		MemoryRequiredBytes:  20 * gib,
		StorageRequiredBytes: 372 * gib,
		RequiresAccelerator:  true,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "MIT",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsageCommercial,
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
		},
		Runtime: catalogue.RuntimeStreaming,
		StreamingRoster: catalogue.RosterMembership{
			FamilyName: StreamingFamilyDeepSeekR1,
			Listed:     true,
		},
		ExpectedCapability: catalogue.ExpectedCapability{
			ContextTokens:             65536,
			ThroughputTokensPerSecond: 3.4,
			Modalities:                []string{"text"},
		},
		Integrity: catalogue.IntegrityExpectation{
			Algorithm: catalogue.DigestSHA256,
			Digest:    "0a5e93c186df427b0c8e5719ad3f6420b8c1de7395024fa6b8e07d1c3925fa64",
			SizeBytes: 372 * gib,
		},
	}
}

// StreamingIneligibleMoEEntry is the most important fixture of the four.
//
// It is architecturally mixture-of-experts, exactly like
// StreamingRosterMemberEntry, and it is NOT on the streaming runtime's roster —
// Qwen3-30B-A3B is among the MoE models the runtime names as unsupported. Any
// implementation that decides streaming eligibility from architecture will
// wrongly offer it, and that offer fails at load time instead of selection time
// (D1). It is served by the in-memory runtime, which is the only path it has.
func StreamingIneligibleMoEEntry() catalogue.Entry {
	return catalogue.Entry{
		ModelID:      "qwen3-30b-a3b",
		Variant:      "q4_k_m",
		Family:       catalogue.FamilyText,
		Architecture: catalogue.ArchitectureMixtureOfExperts,
		Descriptor: catalogue.Descriptor{
			ParameterCount:   30_500_000_000,
			ActiveParameters: 3_300_000_000,
			Quantisation:     "q4_k_m",
			Specialisations:  []string{"reasoning", "multilingual"},
		},
		MemoryRequiredBytes:  20 * gib,
		StorageRequiredBytes: 18*gib + 600*mib,
		RequiresAccelerator:  false,
		UsageTerms: catalogue.UsageTerms{
			LicenseID: "Apache-2.0",
			Permitted: []catalogue.UsagePurpose{
				catalogue.UsageCommercial,
				catalogue.UsagePersonal,
				catalogue.UsageResearch,
				catalogue.UsageEvaluation,
			},
		},
		Runtime: catalogue.RuntimeInMemory,
		// Roster lookup MISSED. The family name is recorded so a reader can see
		// which name was looked up and that the answer was "not listed" — not
		// that no lookup happened.
		StreamingRoster: catalogue.RosterMembership{
			FamilyName: StreamingFamilyQwen3MoE,
			Listed:     false,
		},
		ExpectedCapability: catalogue.ExpectedCapability{
			ContextTokens:             32768,
			ThroughputTokensPerSecond: 11.2,
			Modalities:                []string{"text"},
		},
		Integrity: catalogue.IntegrityExpectation{
			Algorithm: catalogue.DigestSHA256,
			Digest:    "c41b7e26908da35f1728be04c9d6350fa87e1b204c6d95837fe0a2b41d709c58",
			SizeBytes: 18*gib + 600*mib,
		},
	}
}

// Entries returns every fixture entry, in a stable order, for sweeps that must
// hold across all four kinds.
func Entries() []catalogue.Entry {
	return []catalogue.Entry{
		CommercialSafeEntry(),
		NonCommercialEntry(),
		RevenueCappedEntry(),
		StreamingRosterMemberEntry(),
		StreamingIneligibleMoEEntry(),
	}
}
