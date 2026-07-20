package knowledge

import (
	"fmt"
	"regexp"

	"github.com/picoaide/picoaide/internal/store"
)

var (
	wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)
	tagRe      = regexp.MustCompile(`(?:^|[ \t])#(\pL[\pL\pN_-]*)`)
)

func ExtractWikiLinks(content string) []string {
	matches := wikiLinkRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	keywords := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		kw := m[1]
		if !seen[kw] {
			seen[kw] = true
			keywords = append(keywords, kw)
		}
	}
	return keywords
}

func ExtractTags(content string) []string {
	matches := tagRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	tags := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		tag := m[1]
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

type Linker struct{}

func NewLinker() *Linker {
	return &Linker{}
}

func (l *Linker) Rebuild(kbID int64) ([]store.KBLink, []store.KBTag, error) {
	docs, err := store.GetDocumentsByKB(kbID)
	if err != nil {
		return nil, nil, fmt.Errorf("rebuild: get docs: %w", err)
	}

	titleMap := make(map[string]int64, len(docs))
	for _, d := range docs {
		titleMap[d.Title] = d.ID
	}

	var links []store.KBLink
	linkSeen := make(map[string]bool)
	for _, d := range docs {
		for _, kw := range ExtractWikiLinks(d.Content) {
			targetID, ok := titleMap[kw]
			if !ok {
				continue
			}
			key := fmt.Sprintf("%d-%d", d.ID, targetID)
			if linkSeen[key] {
				continue
			}
			linkSeen[key] = true
			links = append(links, store.KBLink{SourceDoc: d.ID, TargetDoc: targetID, Keyword: kw})
		}
	}

	var tags []store.KBTag
	tagSeen := make(map[string]bool)
	for _, d := range docs {
		for _, tag := range ExtractTags(d.Content) {
			key := fmt.Sprintf("%d-%s", d.ID, tag)
			if tagSeen[key] {
				continue
			}
			tagSeen[key] = true
			tags = append(tags, store.KBTag{DocID: d.ID, Tag: tag})
		}
	}

	return links, tags, nil
}

func (l *Linker) RebuildAndStore(kbID int64) error {
	links, tags, err := l.Rebuild(kbID)
	if err != nil {
		return err
	}
	return store.RebuildLinksAndTags(kbID, links, tags)
}

func (l *Linker) MatchKeywords(kbID int64, keywords []string) ([]store.KBLink, error) {
	docs, err := store.GetDocumentsByKB(kbID)
	if err != nil {
		return nil, fmt.Errorf("match keywords: get docs: %w", err)
	}
	titleMap := make(map[string]int64, len(docs))
	for _, d := range docs {
		titleMap[d.Title] = d.ID
	}

	var links []store.KBLink
	seen := make(map[string]bool)
	for _, kw := range keywords {
		targetID, ok := titleMap[kw]
		if !ok {
			continue
		}
		key := fmt.Sprintf("%d", targetID)
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, store.KBLink{Keyword: kw, TargetDoc: targetID})
	}
	return links, nil
}
