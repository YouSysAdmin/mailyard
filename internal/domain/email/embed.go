// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"regexp"
	"strings"

	templatedomain "github.com/yousysadmin/mailyard/internal/domain/template"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// embeddedSuffix ends the Content-ID of every embedded builder image.
const embeddedSuffix = "@mailyard"

// assetRefPattern matches a builder image URL under base where a mail
// client loads it: a src attribute or a CSS url(). Groups are the
// opener, the token and the character that ends the URL, so a URL
// carrying a query or a fragment is not one of ours.
func assetRefPattern(base string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(\bsrc\s*=\s*["']?|url\(\s*(?:["']|&quot;|&#34;|&#39;)?)` +
		regexp.QuoteMeta(base+templatedomain.AssetPath+"/") +
		`([A-Za-z0-9_-]{1,64})(["'\s)>&]|$)`)
}

// embedAssets rewrites every builder image of projID that the HTML
// loads from the hosted URL to a cid: reference, and appends one
// attachment entry per image naming the stored asset. Tokens of another
// project, or of no image, are left as URLs. Without a public URL there
// is nothing to recognise and the HTML is left alone.
func (s *Service) embedAssets(ctx context.Context, projID string, req *SendRequest) error {
	base := strings.TrimRight(s.PublicURL, "/")
	if base == "" || req.HTML == "" {
		return nil
	}

	re := assetRefPattern(base)
	var tokens []string
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(req.HTML, -1) {
		if !seen[m[2]] {
			seen[m[2]] = true
			tokens = append(tokens, m[2])
		}
	}

	if len(tokens) == 0 {
		return nil
	}

	assets, err := s.Store.Template.AssetsByTokens(ctx, projID, tokens)
	if err != nil {
		return err
	}

	byToken := make(map[string]*tmodel.Asset, len(assets))
	for _, a := range assets {
		byToken[a.PublicToken] = a
	}

	cids := make(map[string]string, len(assets))
	for _, tok := range tokens {
		a, ok := byToken[tok]
		if !ok {
			continue
		}

		cid := a.ID + embeddedSuffix
		cids[a.PublicToken] = cid
		req.Attachments = append(req.Attachments, emailmodel.Attachment{
			Filename:        a.Filename,
			ContentType:     a.ContentType,
			Size:            a.Size,
			ContentID:       cid,
			TemplateAssetID: a.ID,
		})
	}

	req.HTML = re.ReplaceAllStringFunc(req.HTML, func(whole string) string {
		m := re.FindStringSubmatch(whole)
		cid, ok := cids[m[2]]
		if !ok {
			return whole
		}

		return m[1] + "cid:" + cid + m[3]
	})

	return nil
}
