package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrSourceSelectionInvalid    = errors.New("source selection is invalid")
	ErrSourceSelectionUnreadable = errors.New("selected source is unreadable")
)

type PlatformSourceSelection struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Path      string           `json:"path" minLength:"1" maxLength:"32768"`
	Bookmark  string           `json:"bookmark,omitempty" maxLength:"65536"`
}

type SourceAccess struct {
	media   *application.Media
	custody sourceMaterialRepository
}

type sourceMaterialRepository interface {
	ReadAssetSourceMaterial(context.Context, domain.AssetID) (domain.SourceGrantSummary, []byte, error)
}

type resolvedAssetSource struct {
	Path        string
	Observation domain.SourceObservation
}

func NewSourceAccess(media *application.Media, custody sourceMaterialRepository) (*SourceAccess, error) {
	if media == nil || custody == nil {
		return nil, fmt.Errorf("source access requires media application and custody repository")
	}
	return &SourceAccess{media: media, custody: custody}, nil
}

func (access *SourceAccess) RegisterSelection(
	ctx context.Context,
	selection PlatformSourceSelection,
) (application.SourceGrantResult, error) {
	if _, err := domain.ParseRequestID(selection.RequestID.String()); err != nil || selection.Path == "" {
		return application.SourceGrantResult{}, ErrSourceSelectionInvalid
	}
	path, err := filepath.Abs(selection.Path)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return application.SourceGrantResult{}, ErrSourceSelectionInvalid
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return application.SourceGrantResult{}, ErrSourceSelectionUnreadable
	}
	file, err := os.Open(resolved)
	if err != nil {
		return application.SourceGrantResult{}, ErrSourceSelectionUnreadable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
		return application.SourceGrantResult{}, ErrSourceSelectionInvalid
	}
	identity, err := sourceFileIdentity(file, info)
	if err != nil {
		return application.SourceGrantResult{}, ErrSourceSelectionUnreadable
	}
	size, err := domain.NewUInt64(uint64(info.Size()))
	if err != nil {
		return application.SourceGrantResult{}, ErrSourceSelectionInvalid
	}
	platform, err := productPlatform(runtime.GOOS)
	if err != nil {
		return application.SourceGrantResult{}, err
	}
	kind := domain.SourceGrantLocalPath
	if selection.Bookmark != "" {
		if platform != "mac" {
			return application.SourceGrantResult{}, ErrSourceSelectionInvalid
		}
		kind = domain.SourceGrantMacBookmark
	}
	material, err := json.Marshal(struct {
		Schema   string `json:"schema"`
		Path     string `json:"path"`
		Bookmark string `json:"bookmark,omitempty"`
	}{Schema: "open-cut/source-grant-material/" + string(kind), Path: resolved, Bookmark: selection.Bookmark})
	if err != nil {
		return application.SourceGrantResult{}, err
	}
	return access.media.RegisterSourceGrant(ctx, application.RegisterSourceGrantInput{
		RequestID: selection.RequestID, Platform: platform, Kind: kind,
		DisplayName: filepath.Base(resolved), ProtectedMaterial: material,
		Observation: domain.SourceObservation{
			ByteSize: size, ModifiedUnixNs: domain.NewInt64(info.ModTime().UnixNano()), FileIdentity: identity,
		},
	})
}

func (access *SourceAccess) resolveAssetSource(
	ctx context.Context,
	assetID domain.AssetID,
) (resolvedAssetSource, error) {
	if assetID.IsZero() {
		return resolvedAssetSource{}, ErrSourceSelectionInvalid
	}
	grant, protected, err := access.custody.ReadAssetSourceMaterial(ctx, assetID)
	if err != nil {
		return resolvedAssetSource{}, err
	}
	var material struct {
		Schema   string `json:"schema"`
		Path     string `json:"path"`
		Bookmark string `json:"bookmark,omitempty"`
	}
	if len(protected) == 0 || len(protected) > maximumProtectedSourceMaterial ||
		json.Unmarshal(protected, &material) != nil ||
		material.Schema != "open-cut/source-grant-material/"+string(grant.Kind) ||
		material.Path == "" || !filepath.IsAbs(material.Path) || filepath.Clean(material.Path) != material.Path {
		return resolvedAssetSource{}, ErrSourceSelectionInvalid
	}
	if grant.Kind == domain.SourceGrantLocalPath && material.Bookmark != "" {
		return resolvedAssetSource{}, ErrSourceSelectionInvalid
	}
	resolved, err := filepath.EvalSymlinks(material.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
				"source-missing", domain.AssetMissing, application.ErrMediaSourceRead,
			)
		}
		return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	if resolved != material.Path {
		return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
		return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	observation, err := sourceObservation(file, info)
	if err != nil {
		return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	if observation != grant.Observation {
		return resolvedAssetSource{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	return resolvedAssetSource{Path: resolved, Observation: observation}, nil
}

func sourceObservation(file *os.File, info os.FileInfo) (domain.SourceObservation, error) {
	identity, err := sourceFileIdentity(file, info)
	if err != nil || info.Size() < 0 {
		return domain.SourceObservation{}, ErrSourceSelectionUnreadable
	}
	size, err := domain.NewUInt64(uint64(info.Size()))
	if err != nil {
		return domain.SourceObservation{}, ErrSourceSelectionInvalid
	}
	return domain.SourceObservation{
		ByteSize: size, ModifiedUnixNs: domain.NewInt64(info.ModTime().UnixNano()), FileIdentity: identity,
	}, nil
}

const maximumProtectedSourceMaterial = 64 << 10

func productPlatform(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "mac", nil
	case "windows":
		return "win", nil
	case "linux":
		return "linux", nil
	default:
		return "", ErrSourceSelectionInvalid
	}
}
