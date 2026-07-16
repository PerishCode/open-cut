package main

import (
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/internal/productresource"
	"github.com/PerishCode/open-cut/product/application"
)

func sequencePreviewRendererIdentity(
	verified mediatoolchain.Verified,
) (application.SequencePreviewRendererIdentity, bool) {
	capability, exists := verified.Capabilities[mediatoolchain.CapabilitySequencePreviewRendererV1]
	if !exists {
		return application.SequencePreviewRendererIdentity{}, false
	}
	return application.SequencePreviewRendererIdentity{
		Version: verified.Manifest.Version + "/" + application.SequencePreviewRendererV1 + "@" +
			capability.ClosureSHA256 + "@" + verified.Manifest.Build.RecipeSHA256,
		Target: verified.Manifest.Target.String(),
	}, true
}

func sequenceExportRendererIdentity(
	verified mediatoolchain.Verified,
) (application.RenderExecutorIdentity, bool) {
	capability, exists := verified.Capabilities[mediatoolchain.CapabilitySequenceExportRendererV1]
	if !exists {
		return application.RenderExecutorIdentity{}, false
	}
	return application.RenderExecutorIdentity{
		Version: verified.Manifest.Version + "/" + application.SequenceExportRendererV1 + "@" +
			capability.ClosureSHA256 + "@" + verified.Manifest.Build.RecipeSHA256,
		Target: verified.Manifest.Target.String(),
	}, true
}

func transcriptCatalogCompatible(resources productresource.Verified) bool {
	for _, entry := range resources.Entries {
		if entry.Name == application.TranscriptProfile && entry.Profile == application.TranscriptProfile {
			return true
		}
	}
	return false
}
