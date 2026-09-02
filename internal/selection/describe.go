package selection

import (
	"strconv"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
)

// This file composes what a caller needs in order to TELL a user what happened.
//
// It composes no sentences. Every value it emits is a machine key drawn from a
// closed enumeration, or a figure taken from the measurement or the catalogue.
// Rendering those into wording is the presentation boundary's job, in the
// user's language: a fixed English string here would be asked identically of a
// Serbian, Japanese or Spanish reader, and would be unusable to all three
// (CONST-046).

// FieldKey names one datum in a composed description. The set is closed so a
// presentation layer can exhaustively map keys to wording and be told at
// compile time when a new one appears.
type FieldKey string

const (
	FieldReason         FieldKey = "reason"
	FieldRemedy         FieldKey = "remedy"
	FieldHost           FieldKey = "host"
	FieldModel          FieldKey = "model"
	FieldVariant        FieldKey = "variant"
	FieldIdentity       FieldKey = "identity"
	FieldFamily         FieldKey = "family"
	FieldRuntime        FieldKey = "runtime"
	FieldResource       FieldKey = "resource"
	FieldRequiredBytes  FieldKey = "required_bytes"
	FieldAvailableBytes FieldKey = "available_bytes"
	FieldReservedBytes  FieldKey = "reserved_bytes"
	FieldShortfallBytes FieldKey = "shortfall_bytes"
	FieldRequirement    FieldKey = "requirement"
	FieldDetail         FieldKey = "detail"
	FieldPurpose        FieldKey = "purpose"
	FieldLicense        FieldKey = "license"
	FieldTerm           FieldKey = "term"
	FieldGranted        FieldKey = "granted"
	FieldThresholdValue FieldKey = "threshold_value"
	FieldThresholdUnit  FieldKey = "threshold_currency"
	FieldThresholdPer   FieldKey = "threshold_period"
	FieldReference      FieldKey = "reference"
	FieldContextTokens  FieldKey = "context_tokens"
	FieldThroughput     FieldKey = "throughput_tokens_per_second"
	FieldMemoryLeft     FieldKey = "memory_remaining_bytes"
	FieldStorageLeft    FieldKey = "storage_remaining_bytes"
	FieldMemoryLeftPct  FieldKey = "memory_remaining_percent"
	FieldMeasuredAt     FieldKey = "measured_at"
	FieldAgeSeconds     FieldKey = "age_seconds"
	FieldMaxAgeSeconds  FieldKey = "max_age_seconds"
	FieldCause          FieldKey = "cause"
)

var knownFieldKeys = map[FieldKey]struct{}{
	FieldReason: {}, FieldRemedy: {}, FieldHost: {}, FieldModel: {},
	FieldVariant: {}, FieldIdentity: {}, FieldFamily: {}, FieldRuntime: {},
	FieldResource: {}, FieldRequiredBytes: {}, FieldAvailableBytes: {},
	FieldReservedBytes: {}, FieldShortfallBytes: {}, FieldRequirement: {},
	FieldDetail: {}, FieldPurpose: {}, FieldLicense: {}, FieldTerm: {},
	FieldGranted: {}, FieldThresholdValue: {}, FieldThresholdUnit: {},
	FieldThresholdPer: {}, FieldReference: {}, FieldContextTokens: {},
	FieldThroughput: {}, FieldMemoryLeft: {}, FieldStorageLeft: {},
	FieldMemoryLeftPct: {}, FieldMeasuredAt: {}, FieldAgeSeconds: {},
	FieldMaxAgeSeconds: {}, FieldCause: {},
}

// Known reports whether k is a recorded field key.
func (k FieldKey) Known() bool {
	_, ok := knownFieldKeys[k]
	return ok
}

// Field is one key and its value, both drawn from data.
type Field struct {
	Key   FieldKey
	Value string
}

// fieldSet accumulates fields in a stable order, dropping empty values so a
// description never carries a key with nothing behind it.
type fieldSet []Field

func (fs *fieldSet) add(k FieldKey, v string) {
	if v == "" {
		return
	}
	*fs = append(*fs, Field{Key: k, Value: v})
}

func (fs *fieldSet) addUint(k FieldKey, v uint64) {
	*fs = append(*fs, Field{Key: k, Value: strconv.FormatUint(v, 10)})
}

func (fs *fieldSet) addInt(k FieldKey, v int) {
	if v == 0 {
		return
	}
	*fs = append(*fs, Field{Key: k, Value: strconv.Itoa(v)})
}

func (fs *fieldSet) addFloat(k FieldKey, v float64) {
	*fs = append(*fs, Field{Key: k, Value: strconv.FormatFloat(v, 'f', -1, 64)})
}

func (fs *fieldSet) addBool(k FieldKey, v bool) {
	*fs = append(*fs, Field{Key: k, Value: strconv.FormatBool(v)})
}

// DescribeWithheld composes the facts behind one withholding: the reason, the
// remedy it implies, which host and model it concerns, and the detail belonging
// to that reason and no other.
func DescribeWithheld(p capability.HostCapabilityProfile, w Withheld) []Field {
	var fs fieldSet
	fs.add(FieldReason, string(w.Reason))
	fs.add(FieldRemedy, string(w.Reason.Remedy()))
	fs.add(FieldHost, p.HostIdentity)
	fs.add(FieldModel, w.ModelID)
	fs.add(FieldVariant, w.Variant)
	fs.add(FieldFamily, string(w.Family))

	switch {
	case w.Shortfall != nil:
		s := w.Shortfall
		fs.add(FieldResource, string(s.Resource))
		fs.addUint(FieldRequiredBytes, s.RequiredBytes)
		fs.addUint(FieldAvailableBytes, s.AvailableBytes)
		fs.addUint(FieldReservedBytes, s.ReservedBytes)
		if s.RequiredBytes > s.AvailableBytes {
			fs.addUint(FieldShortfallBytes, s.RequiredBytes-s.AvailableBytes)
		}
	case w.Unsupported != nil:
		fs.add(FieldRequirement, string(w.Unsupported.Requirement))
		fs.add(FieldDetail, w.Unsupported.Detail)
	case w.Exclusion != nil:
		x := w.Exclusion
		fs.add(FieldPurpose, string(x.Purpose))
		fs.add(FieldLicense, x.LicenseID)
		fs.add(FieldTerm, string(x.Term))
		fs.addBool(FieldGranted, x.Granted)
		if !x.Threshold.Zero() {
			fs.addUint(FieldThresholdValue, x.Threshold.Value)
			fs.add(FieldThresholdUnit, x.Threshold.Currency)
			fs.add(FieldThresholdPer, x.Threshold.Period)
		}
		fs.add(FieldReference, x.Reference)
	}

	return fs
}

// DescribeOption composes what an offered option costs and delivers, in the
// terms the host is measured in, so two options can be weighed against each
// other without knowing what is inside either (FR-005).
func DescribeOption(o Option) []Field {
	var fs fieldSet
	fs.add(FieldHost, o.HostIdentity)
	fs.add(FieldModel, o.ModelID)
	fs.add(FieldVariant, o.Variant)
	fs.add(FieldIdentity, o.Identity)
	fs.add(FieldFamily, string(o.Family))
	fs.add(FieldRuntime, string(o.Runtime))
	fs.addUint(FieldRequiredBytes, o.Cost.MemoryRequiredBytes)
	fs.addUint(FieldStorageLeft, o.Headroom.StorageRemainingBytes)
	fs.addUint(FieldMemoryLeft, o.Headroom.MemoryRemainingBytes)
	fs.addFloat(FieldMemoryLeftPct, o.Headroom.MemoryRemainingFraction*100)
	fs.addInt(FieldContextTokens, o.Expected.ContextTokens)
	if o.Expected.ThroughputTokensPerSecond > 0 {
		fs.addFloat(FieldThroughput, o.Expected.ThroughputTokensPerSecond)
	}
	fs.add(FieldLicense, o.Terms.LicenseID)
	return fs
}

// DescribeFamilyRefusal composes why a family could not be served and what its
// candidates lacked.
func DescribeFamilyRefusal(p capability.HostCapabilityProfile, r FamilyRefusal) []Field {
	var fs fieldSet
	fs.add(FieldReason, string(r.Reason))
	fs.add(FieldRemedy, string(r.Reason.Remedy()))
	fs.add(FieldHost, p.HostIdentity)
	fs.add(FieldFamily, string(r.Family))
	for _, name := range r.Missing() {
		fs.add(FieldDetail, name)
	}
	return fs
}

// DescribeHostRefusal composes why no selection could be made at all, carrying
// the measurement facts the refusal rests on rather than asserting them.
func DescribeHostRefusal(r HostRefusal) []Field {
	var fs fieldSet
	fs.add(FieldReason, string(r.Kind))
	fs.add(FieldHost, r.HostIdentity)
	fs.add(FieldCause, r.Cause)
	if !r.MeasuredAt.IsZero() {
		fs.add(FieldMeasuredAt, r.MeasuredAt.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	if r.MaxAgeSeconds > 0 {
		fs.addFloat(FieldAgeSeconds, r.AgeSeconds)
		fs.addFloat(FieldMaxAgeSeconds, r.MaxAgeSeconds)
	}
	return fs
}
