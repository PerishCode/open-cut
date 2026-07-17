package businessacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

type Creator struct {
	CDP *CDPClient
}

func (creator Creator) Bootstrap(ctx context.Context, projectName, fixturePath string) error {
	if creator.CDP == nil {
		return fmt.Errorf("Creator bootstrap requires the installed UI target")
	}
	if !filepath.IsAbs(fixturePath) || filepath.Clean(fixturePath) != fixturePath {
		return fmt.Errorf("Creator fixture path must be clean and absolute")
	}
	if err := creator.wait(ctx, `document.readyState === "complete" && !!document.body`); err != nil {
		return err
	}
	if err := creator.evaluateBoolean(ctx, setTextExpression("Name your story", projectName)); err != nil {
		return fmt.Errorf("set installed Creator Project name: %w", err)
	}
	if err := creator.wait(ctx, fieldValueAndButtonExpression("Name your story", projectName, "Create and open")); err != nil {
		return fmt.Errorf("wait for installed Creator Project form: %w", err)
	}
	if err := creator.evaluateBoolean(ctx, buttonExpression("Create and open", true)); err != nil {
		return fmt.Errorf("create Project through installed Creator: %w", err)
	}
	const sourceFieldSelector = `input[type="file"]:not(:disabled)`
	if err := creator.wait(ctx, textAndSelectorExpression(projectName, sourceFieldSelector)); err != nil {
		return fmt.Errorf("wait for Creator workspace: %w", err)
	}
	var document struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := creator.CDP.Call(ctx, "DOM.getDocument", map[string]any{"depth": 1}, &document); err != nil {
		return err
	}
	var query struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := creator.CDP.Call(ctx, "DOM.querySelector", map[string]any{
		"nodeId": document.Root.NodeID, "selector": sourceFieldSelector,
	}, &query); err != nil {
		return err
	}
	if query.NodeID == 0 {
		return fmt.Errorf("installed Creator does not expose the shared file field")
	}
	if err := creator.CDP.Call(ctx, "DOM.setFileInputFiles", map[string]any{
		"nodeId": query.NodeID, "files": []string{fixturePath},
	}, nil); err != nil {
		return fmt.Errorf("select Creator fixture: %w", err)
	}
	if err := creator.wait(ctx, textExpression(filepath.Base(fixturePath))); err != nil {
		return fmt.Errorf("wait for Creator Asset: %w", err)
	}
	return nil
}

func (creator Creator) ApprovePairing(ctx context.Context) error {
	if err := creator.CDP.Call(ctx, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
		return err
	}
	if err := creator.openTab(ctx, "Agent"); err != nil {
		return fmt.Errorf("open Creator Agent panel: %w", err)
	}
	if err := creator.wait(ctx, buttonExpression("Approve CLI", false)); err != nil {
		return fmt.Errorf("wait for pending CLI pairing: %w", err)
	}
	if err := creator.evaluateBoolean(ctx, buttonExpression("Approve CLI", true)); err != nil {
		return fmt.Errorf("approve CLI pairing through Creator: %w", err)
	}
	if err := creator.wait(ctx, textExpression("Active key")); err != nil {
		return fmt.Errorf("wait for active CLI pairing: %w", err)
	}
	return nil
}

func (creator Creator) AcquireTranscriptionModel(ctx context.Context) error {
	if creator.CDP == nil {
		return fmt.Errorf("Creator resource acquisition requires the installed UI target")
	}
	if err := creator.openTab(ctx, "System"); err != nil {
		return fmt.Errorf("open Creator System panel: %w", err)
	}
	if err := creator.wait(ctx, buttonExpression("Download for offline use", false)); err != nil {
		return fmt.Errorf("wait for Creator transcription resource action: %w", err)
	}
	if err := creator.evaluateBoolean(ctx, buttonExpression("Download for offline use", true)); err != nil {
		return fmt.Errorf("authorize production transcription model through Creator: %w", err)
	}
	if err := poll(ctx, 750*time.Millisecond, func() (bool, error) {
		value, evaluateErr := creator.evaluate(ctx, `(() => {
  const text = document.body?.innerText ?? "";
  if (text.includes("Acquisition failed ·") || text.includes("download failed")) return "failed";
  if (text.includes("ready offline")) return "ready";
  return "waiting";
})()`)
		if evaluateErr != nil {
			return false, nil
		}
		status, _ := value.(string)
		if status == "failed" {
			return false, fmt.Errorf("production transcription model acquisition failed")
		}
		return status == "ready", nil
	}); err != nil {
		return fmt.Errorf("wait for production transcription model: %w", err)
	}
	return nil
}

// openTab activates a workspace panel exactly as a human does: the panes
// behind inactive tabs are unmounted, so every panel interaction selects its
// tab first.
func (creator Creator) openTab(ctx context.Context, label string) error {
	if err := creator.wait(ctx, tabExpression(label, false)); err != nil {
		return err
	}
	return creator.evaluateBoolean(ctx, tabExpression(label, true))
}

func (creator Creator) wait(ctx context.Context, expression string) error {
	return poll(ctx, 100*time.Millisecond, func() (bool, error) {
		value, err := creator.evaluate(ctx, expression)
		if err != nil {
			return false, nil
		}
		ready, _ := value.(bool)
		return ready, nil
	})
}

func (creator Creator) evaluateBoolean(ctx context.Context, expression string) error {
	value, err := creator.evaluate(ctx, expression)
	if err != nil {
		return err
	}
	if ready, _ := value.(bool); !ready {
		return fmt.Errorf("Creator gesture target is unavailable")
	}
	return nil
}

func (creator Creator) evaluate(ctx context.Context, expression string) (any, error) {
	var response struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := creator.CDP.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expression, "returnByValue": true, "awaitPromise": true,
	}, &response); err != nil {
		return nil, err
	}
	if len(response.Exception) > 0 && string(response.Exception) != "null" {
		return nil, fmt.Errorf("Creator expression raised an exception")
	}
	return response.Result.Value, nil
}

func setTextExpression(label, value string) string {
	return fmt.Sprintf(`(() => {
  const field = [...document.querySelectorAll("label")].find((node) => node.textContent?.includes(%s))?.querySelector("input");
  if (!(field instanceof HTMLInputElement)) return false;
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  setter?.call(field, %s);
  field.dispatchEvent(new Event("input", { bubbles: true }));
  field.dispatchEvent(new Event("change", { bubbles: true }));
  return true;
})()`, jsString(label), jsString(value))
}

func fieldValueAndButtonExpression(label, value, button string) string {
	return fmt.Sprintf(`(() => {
  const field = [...document.querySelectorAll("label")].find((node) => node.textContent?.includes(%s))?.querySelector("input");
  const action = [...document.querySelectorAll("button")].find((node) => node.textContent?.trim() === %s && !node.disabled);
  return field instanceof HTMLInputElement && field.value === %s && action instanceof HTMLButtonElement;
})()`, jsString(label), jsString(button), jsString(value))
}

func buttonExpression(text string, click bool) string {
	action := "return true;"
	if click {
		action = "button.click(); return true;"
	}
	return fmt.Sprintf(`(() => {
  const button = [...document.querySelectorAll('button:not([role="tab"])')].find((node) => node.textContent?.trim() === %s && !node.disabled);
  if (!(button instanceof HTMLButtonElement)) return false;
  %s
})()`, jsString(text), action)
}

func tabExpression(text string, click bool) string {
	action := "return true;"
	if click {
		action = "tab.click(); return true;"
	}
	return fmt.Sprintf(`(() => {
  const tab = [...document.querySelectorAll('button[role="tab"]')].find((node) => node.textContent?.trim() === %s && !node.disabled);
  if (!(tab instanceof HTMLButtonElement)) return false;
  %s
})()`, jsString(text), action)
}

func textExpression(text string) string {
	return fmt.Sprintf(`document.body?.innerText.includes(%s) === true`, jsString(text))
}

func textAndSelectorExpression(text, selector string) string {
	return fmt.Sprintf(`document.body?.innerText.includes(%s) === true && !!document.querySelector(%s)`, jsString(text), jsString(selector))
}

func jsString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
