package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Only the scan response ID is retained; authentication headers and cookies are
// never captured. The upload comes from the exact Blob the scanner submitted.
type scanArtifactCapture struct {
	mu         sync.Mutex
	responseID network.RequestID
}

func newScanArtifactCapture(ctx context.Context) *scanArtifactCapture {
	capture := &scanArtifactCapture{}
	chromedp.ListenTarget(ctx, func(event any) {
		response, ok := event.(*network.EventResponseReceived)
		if !ok {
			return
		}
		address, err := url.Parse(response.Response.URL)
		if err != nil || address.Path != "/api/scan" {
			return
		}
		capture.mu.Lock()
		capture.responseID = response.RequestID
		capture.mu.Unlock()
	})
	return capture
}

func (capture *scanArtifactCapture) save(ctx context.Context, directory string, result scanResult) error {
	var dataURL string
	var metadata json.RawMessage
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {const blob=Alpine.$data(document.querySelector('.scanner-shell')).lastScanBlob;if(!blob)throw new Error('No submitted scan Blob');return new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=()=>reject(reader.error);reader.readAsDataURL(blob)})})()`, &dataURL, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) }),
		chromedp.Evaluate(`(() => {const state=Alpine.$data(document.querySelector('.scanner-shell'));const image=document.querySelector('.scanner-frame img');return {lines:{left:state.lines.left,right:state.lines.right,top:state.lines.top,bottom:state.lines.bottom},rotation:state.previewRotation,game:state.game,language:state.lang,needs_review:state.needsReview,match_confirmed:state.matchConfirmed,uploaded_bytes:state.lastScanBlob?.size,uploaded_type:state.lastScanBlob?.type,preview_rendered:Boolean(image?.complete&&image?.naturalWidth),viewport:{width:innerWidth,height:innerHeight}}})()`, &metadata),
	); err != nil {
		return err
	}
	_, encoded, found := strings.Cut(dataURL, ",")
	if !found {
		return fmt.Errorf("submitted Blob is not a data URL")
	}
	upload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode submitted Blob: %w", err)
	}
	capture.mu.Lock()
	responseID := capture.responseID
	capture.mu.Unlock()
	if responseID == "" {
		return fmt.Errorf("no actual /api/scan response was observed")
	}
	var response []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		response, err = network.GetResponseBody(responseID).Do(ctx)
		return err
	})); err != nil {
		return fmt.Errorf("read actual scan response: %w", err)
	}
	if err := os.MkdirAll(directory, 0750); err != nil {
		return err
	}
	rendered, err := json.MarshalIndent(struct {
		Result  scanResult      `json:"rendered_result"`
		Browser json.RawMessage `json:"browser"`
		SHA256  string          `json:"upload_sha256"`
	}{result, metadata, fmt.Sprintf("%x", sha256.Sum256(upload))}, "", "  ")
	if err != nil {
		return err
	}
	for name, contents := range map[string][]byte{"upload-crop.jpg": upload, "scan-response.json": response, "rendered-result.json": rendered} {
		if err := os.WriteFile(filepath.Join(directory, name), contents, 0600); err != nil {
			return err
		}
	}
	return nil
}
