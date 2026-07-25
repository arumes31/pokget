## 2024-05-24 - Efficient HTTP response decoding
**Learning:** Decoding JSON directly from the http.Response.Body stream is more performant than using io.ReadAll to read into memory and then json.Unmarshal.
**Action:** When decoding JSON from an `io.Reader` (like an `http.Response.Body`), prefer `json.NewDecoder(reader).Decode(&dest)` over `io.ReadAll(reader)` followed by `json.Unmarshal()`. Ensure the remaining body is explicitly drained using `_, _ = io.Copy(io.Discard, resp.Body)` to allow HTTP transport keep-alive reuse.
