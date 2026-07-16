package application

import (
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestInitialMediaJobsCarryClosedSemanticProfiles(t *testing.T) {
	assetValue, err := domain.GenerateUUIDv7(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	assetID, _ := domain.ParseAssetID(assetValue)
	kinds := []domain.MediaJobKind{
		domain.MediaJobIdentify, domain.MediaJobProbe, domain.MediaJobProxy,
		domain.MediaJobWaveform, domain.MediaJobTranscript,
	}
	ids := make([]domain.MediaJobID, len(kinds))
	for index := range ids {
		value, generateErr := domain.GenerateUUIDv7(time.Now().UTC())
		if generateErr != nil {
			t.Fatal(generateErr)
		}
		ids[index], _ = domain.ParseMediaJobID(value)
	}
	jobs, err := buildInitialMediaJobs(ids, assetID)
	if err != nil {
		t.Fatal(err)
	}
	for index, job := range jobs {
		parameters, decodeErr := DecodeInitialMediaJobParameters(job.ParametersJSON)
		profile, profileErr := InitialMediaProfile(kinds[index])
		if decodeErr != nil || profileErr != nil || parameters.AssetID != assetID ||
			parameters.Kind != kinds[index] || parameters.Profile != profile {
			t.Fatalf("job[%d]=%+v parameters=%+v decode=%v profile=%v", index, job, parameters, decodeErr, profileErr)
		}
		canonical, digest, canonicalErr := CanonicalInitialMediaJobParameters(parameters)
		if canonicalErr != nil || string(canonical) != string(job.ParametersJSON) || digest != job.ParametersDigest {
			t.Fatalf("job[%d] parameters are not canonical", index)
		}
		if kinds[index] == domain.MediaJobProxy {
			if parameters.ProxySelection == nil || parameters.ProxySelection.Policy != SourceProxySelectionDefault {
				t.Fatalf("proxy prewarm selection=%+v", parameters.ProxySelection)
			}
		} else if parameters.ProxySelection != nil {
			t.Fatalf("non-proxy job carries proxy selection: %+v", parameters)
		}
	}
}
