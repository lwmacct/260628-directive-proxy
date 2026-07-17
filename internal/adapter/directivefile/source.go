package directivefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

type Options struct {
	Root            string
	MaxPayloadBytes int64
}

type Source struct {
	root            string
	maxPayloadBytes int64
}

var errFileTooLarge = errors.New("directive file exceeds limit")

var _ directive.FileRemoteReader = (*Source)(nil)

func New(opts Options) *Source {
	return &Source{root: strings.TrimSpace(opts.Root), maxPayloadBytes: opts.MaxPayloadBytes}
}

func (s *Source) Read(ctx context.Context, reference directive.FileReference) ([]byte, error) {
	if s == nil || s.root == "" {
		return nil, directive.ErrRemoteUnavailable
	}
	if reference.Path == "." || strings.Contains(reference.Path, "\\") || !fs.ValidPath(reference.Path) {
		return nil, directive.ErrRemoteInvalid
	}
	path, err := filepath.Localize(reference.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: localize file path %q: %w", directive.ErrRemoteInvalid, reference.Path, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", directive.ErrRemoteUnavailable, err)
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, fmt.Errorf("%w: open directive root %q: %w", directive.ErrRemoteUnavailable, s.root, err)
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, directive.ErrRemoteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: open directive file %q from %q: %w", directive.ErrRemoteUnavailable, reference.Path, s.root, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: stat directive file %q from %q: %w", directive.ErrRemoteUnavailable, reference.Path, s.root, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: directive path %q from %q is not a regular file", directive.ErrRemoteInvalid, reference.Path, s.root)
	}
	if s.maxPayloadBytes > 0 && info.Size() > s.maxPayloadBytes {
		return nil, fmt.Errorf("%w: directive file %q from %q exceeds limit", directive.ErrRemoteInvalid, reference.Path, s.root)
	}
	value, err := readBounded(file, s.maxPayloadBytes)
	if errors.Is(err, errFileTooLarge) {
		return nil, fmt.Errorf("%w: directive file %q from %q exceeds limit", directive.ErrRemoteInvalid, reference.Path, s.root)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read directive file %q from %q: %w", directive.ErrRemoteUnavailable, reference.Path, s.root, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", directive.ErrRemoteUnavailable, err)
	}
	return value, nil
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(reader)
	}
	value, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > maxBytes {
		return nil, errFileTooLarge
	}
	return value, nil
}
