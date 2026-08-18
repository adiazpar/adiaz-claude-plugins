package knowledge

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	closureArchiveNavigationBlock    = "campaign-archives"
	closureProjectionNavigationBlock = "closure-projections"
)

type closureArchiveNavigationEntry struct {
	Destination string
	CampaignID  string
}

func (service *Service) prepareClosureNavigationArtifacts(
	campaign CampaignRecord,
	job ClosureJob,
	manifest ArchiveManifest,
	request ClosureApplyRequest,
) ([]StateArtifactWrite, error) {
	archives, err := service.closureArchiveNavigationEntries(
		closureArchiveNavigationEntry{Destination: job.ArchiveDestination, CampaignID: campaign.ID})
	if err != nil {
		return nil, err
	}
	projectLines := []string{"## Closed campaign archives", ""}
	historyLines := []string{"## Campaign archives", ""}
	for _, entry := range archives {
		name := path.Base(entry.Destination)
		projectLines = append(projectLines, fmt.Sprintf(
			"- [%s](history/campaigns/%s/README.md) (`%s`)", name, name, entry.CampaignID))
		historyLines = append(historyLines, fmt.Sprintf(
			"- [%s](campaigns/%s/README.md) (`%s`)", name, name, entry.CampaignID))
	}
	if len(archives) == 0 {
		projectLines = append(projectLines, "- No closed campaigns.")
		historyLines = append(historyLines, "- No closed campaigns.")
	}

	artifacts := []StateArtifactWrite{}
	for _, item := range []struct {
		Path  string
		Block string
		Lines []string
	}{
		{"docs/INDEX.md", closureArchiveNavigationBlock, projectLines},
		{"docs/history/INDEX.md", closureArchiveNavigationBlock, historyLines},
	} {
		artifact, err := service.closureManagedNavigationArtifact(item.Path, item.Block, item.Lines, request)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}

	for _, category := range []struct {
		Prefix string
		Index  string
		Title  string
	}{
		{"docs/truth/", "docs/truth/INDEX.md", "Current truth projections"},
		{"docs/backlog/", "docs/backlog/INDEX.md", "Backlog projections"},
		{"docs/playbooks/", "docs/playbooks/INDEX.md", "Playbook projections"},
	} {
		affected := false
		for destination := range manifest.Projections {
			if strings.HasPrefix(destination, category.Prefix) && destination != category.Index {
				affected = true
				break
			}
		}
		if !affected {
			continue
		}
		entries, err := service.closureProjectionNavigationEntries(
			strings.TrimSuffix(category.Prefix, "/"), category.Index, manifest.Projections)
		if err != nil {
			return nil, err
		}
		lines := []string{"## " + category.Title, ""}
		for _, destination := range entries {
			relative, err := filepath.Rel(filepath.Dir(category.Index), destination)
			if err != nil {
				return nil, err
			}
			digest := manifest.Projections[destination]
			if digest == "" {
				body, _, readErr := service.readOptionalClosureIndexSource(destination)
				if readErr != nil {
					return nil, readErr
				}
				digest = "sha256:" + SHA256Bytes(body)
			}
			lines = append(lines, fmt.Sprintf(
				"- [%s](%s) (`%s`)", destination, filepath.ToSlash(relative), digest))
		}
		artifact, err := service.closureManagedNavigationArtifact(
			category.Index, closureProjectionNavigationBlock, lines, request)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func (service *Service) closureArchiveNavigationEntries(
	current closureArchiveNavigationEntry,
) ([]closureArchiveNavigationEntry, error) {
	entries := map[string]closureArchiveNavigationEntry{current.Destination: current}
	root := filepath.Join(service.Boundary.Root, "docs", "history", "campaigns")
	directories, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []closureArchiveNavigationEntry{current}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, directory := range directories {
		if directory.Type()&os.ModeSymlink != 0 || !directory.IsDir() ||
			!managedSlugRE.MatchString(directory.Name()) {
			continue
		}
		destination := path.Join("docs/history/campaigns", directory.Name())
		if destination == current.Destination ||
			!archiveCutoverPublished(service.Boundary, destination, "", "", map[string]bool{}) {
			continue
		}
		manifestPath, err := service.Boundary.Resolve(path.Join(destination, "manifest.json"), true)
		if err != nil {
			return nil, err
		}
		body, err := readSingleLinkRegularFile(manifestPath)
		if err != nil {
			return nil, err
		}
		var manifest ArchiveManifest
		if decodeStrictJSON(body, &manifest) != nil || ValidateArchiveManifest(manifest) != nil {
			return nil, errors.New("published archive manifest failed navigation verification")
		}
		entries[destination] = closureArchiveNavigationEntry{
			Destination: destination, CampaignID: manifest.CampaignID,
		}
	}
	result := make([]closureArchiveNavigationEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Destination < result[j].Destination })
	return result, nil
}

func (service *Service) closureProjectionNavigationEntries(
	rootRelative, indexPath string,
	staged map[string]string,
) ([]string, error) {
	entries := map[string]bool{}
	root := filepath.Join(service.Boundary.Root, filepath.FromSlash(rootRelative))
	info, err := os.Lstat(root)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.New("closure projection navigation root is unsafe")
		}
		err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == root {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("closure projection navigation encountered a symbolic link")
			}
			if entry.IsDir() {
				return nil
			}
			fileInfo, err := entry.Info()
			if err != nil || !fileInfo.Mode().IsRegular() {
				return errors.New("closure projection navigation encountered an unsupported entry")
			}
			relative, err := filepath.Rel(service.Boundary.Root, current)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if relative != indexPath && path.Ext(relative) == ".md" {
				entries[relative] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	prefix := strings.TrimSuffix(rootRelative, "/") + "/"
	for destination := range staged {
		if strings.HasPrefix(destination, prefix) && destination != indexPath {
			entries[destination] = true
		}
	}
	result := make([]string, 0, len(entries))
	for entry := range entries {
		result = append(result, entry)
	}
	sort.Strings(result)
	return result, nil
}

func (service *Service) closureManagedNavigationArtifact(
	relative, block string,
	lines []string,
	request ClosureApplyRequest,
) (StateArtifactWrite, error) {
	body, existingDigest, err := service.readOptionalClosureIndexSource(relative)
	if err != nil {
		return StateArtifactWrite{}, err
	}
	if existingDigest != "" && request.ExpectedArtifactDigests[relative] != existingDigest {
		return StateArtifactWrite{}, fmt.Errorf("navigation index %s requires its exact expected digest", relative)
	}
	if existingDigest == "" && request.ExpectedArtifactDigests[relative] != "" {
		return StateArtifactWrite{}, fmt.Errorf("navigation index %s does not exist at its expected digest", relative)
	}
	rendered, err := replaceClosureManagedBlock(body, block, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return StateArtifactWrite{}, fmt.Errorf("navigation index %s: %w", relative, err)
	}
	return StateArtifactWrite{
		Path: relative, ExpectedDigest: request.ExpectedArtifactDigests[relative],
		ContentDigest: "sha256:" + SHA256Bytes(rendered), Body: rendered,
	}, nil
}

func (service *Service) readOptionalClosureIndexSource(relative string) ([]byte, string, error) {
	if validateRelativeRecordPath(relative) != nil {
		return nil, "", errors.New("closure navigation path is invalid")
	}
	absolute := filepath.Join(service.Boundary.Root, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, "", errors.New("closure navigation source is unsafe")
	}
	body, err := readSingleLinkRegularFile(absolute)
	if err != nil {
		return nil, "", err
	}
	return body, "sha256:" + SHA256Bytes(body), nil
}

func replaceClosureManagedBlock(existing []byte, name, content string) ([]byte, error) {
	start := "<!-- re-discipline:" + name + " -->"
	end := "<!-- re-discipline:" + name + ":end -->"
	text := string(existing)
	startCount, endCount := strings.Count(text, start), strings.Count(text, end)
	if startCount != endCount || startCount > 1 {
		return nil, errors.New("managed navigation markers are unbalanced or repeated")
	}
	block := start + "\n" + content + end + "\n"
	if startCount == 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" && !strings.HasSuffix(text, "\n\n") {
			text += "\n"
		}
		return []byte(text + block), nil
	}
	startIndex := strings.Index(text, start)
	endIndex := strings.Index(text, end)
	if endIndex < startIndex {
		return nil, errors.New("managed navigation markers are reversed")
	}
	endIndex += len(end)
	if endIndex < len(text) && text[endIndex] == '\n' {
		endIndex++
	}
	return []byte(text[:startIndex] + block + text[endIndex:]), nil
}
