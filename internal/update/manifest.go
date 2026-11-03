package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func BuildManifest(dir, version, tag, notes, releaseURL, publishedAt string) (Manifest, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	tag = strings.TrimSpace(tag)
	if tag == "" && version != "" {
		tag = "v" + version
	}
	if version == "" {
		version = strings.TrimPrefix(tag, "v")
	}
	if version == "" || tag == "" {
		return Manifest{}, fmt.Errorf("version and tag are required")
	}
	data, err := os.ReadFile(filepath.Join(dir, checksumAsset))
	if err != nil {
		return Manifest{}, err
	}
	assets := map[string]Asset{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == "" || name == checksumAsset || strings.ContainsAny(name, `/\`) {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return Manifest{}, err
		}
		assets[name] = Asset{Name: name, Size: info.Size(), SHA256: strings.ToLower(fields[0])}
	}
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]Asset, 0, len(names))
	for _, name := range names {
		list = append(list, assets[name])
	}
	return Manifest{
		Version:     version,
		Tag:         tag,
		PublishedAt: strings.TrimSpace(publishedAt),
		ReleaseURL:  strings.TrimSpace(releaseURL),
		Notes:       strings.TrimSpace(notes),
		Assets:      list,
	}, nil
}

func WriteManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
