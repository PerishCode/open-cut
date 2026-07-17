package businessacceptance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

type NativeSaveDialog interface {
	Select(context.Context, string) error
}

func (creator Creator) SaveAndRevealExport(
	ctx context.Context,
	destinationPath string,
	dialog NativeSaveDialog,
) error {
	if creator.CDP == nil || dialog == nil {
		return fmt.Errorf("Creator export delivery requires the installed UI and native dialog driver")
	}
	if !filepath.IsAbs(destinationPath) || filepath.Clean(destinationPath) != destinationPath ||
		filepath.Ext(destinationPath) != ".webm" {
		return fmt.Errorf("Creator export destination must be a clean absolute WebM path")
	}
	if err := creator.openTab(ctx, "Export"); err != nil {
		return fmt.Errorf("open Creator Export panel: %w", err)
	}
	if err := creator.wait(ctx, buttonExpression("Save As…", false)); err != nil {
		return fmt.Errorf("wait for installed Creator Save As action: %w", err)
	}
	if err := creator.evaluateBoolean(ctx, buttonExpression("Save As…", true)); err != nil {
		return fmt.Errorf("start installed Creator Save As: %w", err)
	}
	if err := dialog.Select(ctx, destinationPath); err != nil {
		return fmt.Errorf("select installed Creator export destination: %w", err)
	}
	displayName := filepath.Base(destinationPath)
	if err := creator.wait(ctx, textAndButtonExpression("SAVED "+displayName, "Reveal in folder")); err != nil {
		return fmt.Errorf("wait for installed Creator delivery receipt: %w", err)
	}
	if err := creator.evaluateBoolean(ctx, safeDeliveryProjectionExpression(displayName, filepath.Dir(destinationPath))); err != nil {
		return fmt.Errorf("installed Creator delivery projection exposed platform authority: %w", err)
	}
	if err := creator.evaluateBoolean(ctx, buttonExpression("Reveal in folder", true)); err != nil {
		return fmt.Errorf("reveal installed Creator export: %w", err)
	}
	if err := creator.wait(ctx, textExpression("Revealed "+displayName)); err != nil {
		return fmt.Errorf("wait for installed Creator Reveal result: %w", err)
	}
	return nil
}

func verifyDeliveredExport(path string, observation Observation) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		!positiveDecimal(observation.ExportByteSize) || !validAcceptanceDigest(observation.ExportContentDigest) {
		return fmt.Errorf("installed export delivery evidence is incomplete")
	}
	expectedSize, err := strconv.ParseInt(observation.ExportByteSize, 10, 64)
	if err != nil || expectedSize <= 0 {
		return fmt.Errorf("installed export delivery byte size is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedSize {
		return fmt.Errorf("installed export destination is not the exact regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open installed export destination failed")
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != expectedSize || fmt.Sprintf("sha256:%x", digest.Sum(nil)) != observation.ExportContentDigest {
		return fmt.Errorf("installed export destination failed byte verification")
	}
	return nil
}

func textAndButtonExpression(text, button string) string {
	return fmt.Sprintf(`(() => {
  const content = document.body?.innerText ?? "";
  const action = [...document.querySelectorAll("button")].find((node) => node.textContent?.trim() === %s && !node.disabled);
  return content.includes(%s) && action instanceof HTMLButtonElement;
})()`, jsString(button), jsString(text))
}

func safeDeliveryProjectionExpression(displayName, forbiddenDirectory string) string {
	return fmt.Sprintf(`(() => {
  const content = document.body?.innerText ?? "";
  return content.includes(%s) && !content.includes(%s) && !content.includes("delivery.");
})()`, jsString(displayName), jsString(forbiddenDirectory))
}
