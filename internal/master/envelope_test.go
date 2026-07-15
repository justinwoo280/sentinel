package master

import (
	"encoding/json"
	"testing"
)

// TestUnwrapResultData is a regression test for the bug where OnQuality
// parsed the ctrl.Result envelope ({ok,msg,data}) as if it were the raw
// quality JSON, so Score/Media were always nil and trend entries were
// stored empty.
func TestUnwrapResultData(t *testing.T) {
	quality := `{"Score":{"SCAMALYTICS":"12"},"Media":{"Youtube":{"Status":"Yes","Region":"US"}}}`
	envelope := `{"ok":true,"msg":"quality check completed","data":` + quality + `}`

	got := unwrapResultData(json.RawMessage(envelope))

	var qr struct {
		Score struct {
			SCAMALYTICS *string `json:"SCAMALYTICS"`
		} `json:"Score"`
		Media struct {
			Youtube struct {
				Status *string `json:"Status"`
			} `json:"Youtube"`
		} `json:"Media"`
	}
	if err := json.Unmarshal(got, &qr); err != nil {
		t.Fatalf("unwrapped data did not parse: %v", err)
	}
	if qr.Score.SCAMALYTICS == nil || *qr.Score.SCAMALYTICS != "12" {
		t.Fatalf("scamalytics not extracted: %+v", qr.Score.SCAMALYTICS)
	}
	if qr.Media.Youtube.Status == nil || *qr.Media.Youtube.Status != "Yes" {
		t.Fatalf("youtube status not extracted: %+v", qr.Media.Youtube.Status)
	}
}

func TestUnwrapResultDataPassthrough(t *testing.T) {
	// No "data" field → return input unchanged.
	raw := json.RawMessage(`{"Score":{"SCAMALYTICS":"5"}}`)
	got := unwrapResultData(raw)
	if string(got) != string(raw) {
		t.Fatalf("expected passthrough, got %s", got)
	}
}

func TestResultText(t *testing.T) {
	// Report/log carry text in "msg".
	env := `{"ok":true,"msg":"Node: n1\nRegion: JP"}`
	if got := resultText(json.RawMessage(env)); got != "Node: n1\nRegion: JP" {
		t.Fatalf("resultText(msg) = %q", got)
	}
	// data as a JSON string is preferred.
	env2 := `{"ok":true,"msg":"ignored","data":"the-log-text"}`
	if got := resultText(json.RawMessage(env2)); got != "the-log-text" {
		t.Fatalf("resultText(data) = %q", got)
	}
}
