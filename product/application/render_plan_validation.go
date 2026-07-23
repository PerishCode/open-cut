package application

import (
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

func ValidateSequencePreviewRenderPlanPayload(plan domain.RenderPlanPayload) error {
	return rendercontract.ValidateSequencePreviewRenderPlanPayload(plan)
}

func ValidateSequenceExportRenderPlanPayload(plan domain.RenderPlanPayload) error {
	return rendercontract.ValidateSequenceExportRenderPlanPayload(plan)
}

func ValidateRenderPlanPayload(plan domain.RenderPlanPayload) error {
	return rendercontract.ValidateRenderPlanPayload(plan)
}
