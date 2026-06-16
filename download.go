package telconyx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"sync"
)

// Download downloads a file from Telegram and saves it to dest.
// It overwrites dest if it already exists. Chunked files are reassembled
// in parallel using up to ChunkConcurrency workers.
func (c *Client) Download(ctx context.Context, link *FileLink, dest string) (int64, error) {
	if link == nil {
		return 0, ErrInvalidLink
	}
	if link.FileID == "" {
		return 0, fmt.Errorf("%w: missing file_id", ErrInvalidLink)
	}
	if int64(link.Size) > c.cfg.MaxDownloadSize {
		return 0, fmt.Errorf("telconyx: file size %d exceeds MaxDownloadSize %d: %w",
			link.Size, c.cfg.MaxDownloadSize, ErrDownloadTooLarge)
	}

	chunks := link.AllChunks()
	if len(chunks) > 1 {
		return c.downloadChunkedToFile(ctx, link, chunks, dest)
	}
	return c.downloadSingleToFile(ctx, link, dest)
}

// DownloadTo streams the file content to w. It returns the number of bytes written.
// For chunked files, chunks are downloaded sequentially (since random access on
// a plain io.Writer is not generally possible).
func (c *Client) DownloadTo(ctx context.Context, link *FileLink, w io.Writer) (int64, error) {
	if link == nil {
		return 0, ErrInvalidLink
	}
	if link.FileID == "" {
		return 0, fmt.Errorf("%w: missing file_id", ErrInvalidLink)
	}
	if int64(link.Size) > c.cfg.MaxDownloadSize {
		return 0, fmt.Errorf("telconyx: file size %d exceeds MaxDownloadSize %d: %w",
			link.Size, c.cfg.MaxDownloadSize, ErrDownloadTooLarge)
	}

	chunks := link.AllChunks()
	if len(chunks) > 1 {
		return c.downloadChunkedToWriter(ctx, chunks, w)
	}
	return c.downloadSingleToWriter(ctx, link, w)
}

func (c *Client) downloadSingleToFile(ctx context.Context, link *FileLink, dest string) (int64, error) {
	f, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return c.downloadSingleToWriter(ctx, link, f)
}

func (c *Client) downloadSingleToWriter(ctx context.Context, link *FileLink, w io.Writer) (int64, error) {
	var total int64
	err := c.withRetry(ctx, func(ctx context.Context) error {
		n, err := c.downloadChunkBytes(ctx, link.FileID, 0, w)
		total = n
		return err
	})
	return total, err
}

// downloadChunkedToFile downloads all chunks in parallel and writes them at
// the correct offset in dest.
func (c *Client) downloadChunkedToFile(ctx context.Context, link *FileLink, chunks []ChunkRef, dest string) (int64, error) {
	f, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Pre-allocate to avoid sparse-file issues and disk fragmentation.
	if link.Size > 0 {
		if err := f.Truncate(int64(link.Size)); err != nil {
			return 0, err
		}
	}

	concurrency := c.cfg.ChunkConcurrency
	if concurrency <= 0 {
		concurrency = DefaultChunkConcurrency
	}
	if concurrency > len(chunks) {
		concurrency = len(chunks)
	}

	jobs := make(chan ChunkRef, len(chunks))
	for _, ch := range chunks {
		jobs <- ch
	}
	close(jobs)

	type chunkResult struct {
		idx int
		n   int64
		err error
	}
	results := make(chan chunkResult, len(chunks))
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ch := range jobs {
				data, err := c.downloadChunkToMemory(ctx, ch.FileID)
				results <- chunkResult{idx: ch.Index, n: int64(len(data)), err: err}
				if err != nil {
					continue
				}
				offset := int64(ch.Index) * int64(link.ChunkSize)
				if _, werr := f.WriteAt(data, offset); werr != nil {
					results <- chunkResult{idx: ch.Index, err: fmt.Errorf("write chunk %d: %w", ch.Index, werr)}
				}
			}
		}()
	}

	wg.Wait()
	close(results)

	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("chunk %d: %w", r.idx, r.err)
		}
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return int64(link.Size), nil
}

// downloadChunkedToWriter streams chunks sequentially to w.
// This is slower than the parallel file variant but works with any io.Writer.
func (c *Client) downloadChunkedToWriter(ctx context.Context, chunks []ChunkRef, w io.Writer) (int64, error) {
	var total int64
	for _, ch := range chunks {
		var chunkSize int64
		err := c.withRetry(ctx, func(ctx context.Context) error {
			n, err := c.downloadChunkBytes(ctx, ch.FileID, int64(ch.Size), w)
			chunkSize = n
			return err
		})
		if err != nil {
			return total, fmt.Errorf("chunk %d: %w", ch.Index, err)
		}
		total += chunkSize
	}
	return total, nil
}

// downloadChunkBytes downloads a single chunk and writes its content to w,
// limiting the read to max bytes (use 0 for no limit). Returns the number of
// bytes written. This is the low-level call used by both file and writer
// paths; it is retry-aware at the caller level.
func (c *Client) downloadChunkBytes(ctx context.Context, fileID string, max int64, w io.Writer) (int64, error) {
	params := url.Values{}
	params.Set("file_id", fileID)

	resp, err := c.tp.PostForm(ctx, "getFile", params)
	if err != nil {
		return 0, err
	}
	filePath, err := parseGetFileResponse(resp.Body)
	if err != nil {
		return 0, err
	}

	sr, err := c.tp.GetStream(ctx, c.tp.FileURL(filePath))
	if err != nil {
		return 0, err
	}
	if sr.StatusCode != 200 {
		b, _ := io.ReadAll(sr.Body)
		_ = sr.Body.Close()
		return 0, &APIError{
			Code:        sr.StatusCode,
			Description: fmt.Sprintf("download failed: %s", truncate(string(b), 256)),
		}
	}
	defer sr.Body.Close()

	if max > 0 {
		return io.Copy(w, io.LimitReader(sr.Body, max))
	}
	return io.Copy(w, sr.Body)
}

// downloadChunkToMemory downloads a chunk fully into memory and returns the bytes.
func (c *Client) downloadChunkToMemory(ctx context.Context, fileID string) ([]byte, error) {
	var buf bytes.Buffer
	err := c.withRetry(ctx, func(ctx context.Context) error {
		buf.Reset()
		_, err := c.downloadChunkBytes(ctx, fileID, 0, &buf)
		return err
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
