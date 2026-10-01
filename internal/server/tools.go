package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateInput struct {
	Title     string `json:"title"`
	MediaType string `json:"media_type"`
	HTML      string `json:"html"`
}
type UpdateInput struct {
	ID               string  `json:"id"`
	Title            *string `json:"title,omitempty"`
	MediaType        *string `json:"media_type,omitempty"`
	HTML             *string `json:"html,omitempty"`
	ExpectedRevision *int64  `json:"expected_revision,omitempty"`
}
type ReadInput struct {
	ID        string `json:"id"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}
type PatchInput struct {
	ID               string     `json:"id"`
	ExpectedRevision int64      `json:"expected_revision"`
	Edits            []TextEdit `json:"edits"`
}
type ReadOutput struct {
	Page       PageLink `json:"page"`
	Content    string   `json:"content"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	TotalLines int      `json:"total_lines"`
}
type PatchOutput struct {
	Page         PageLink `json:"page"`
	AppliedEdits int      `json:"applied_edits"`
	DashboardURL string   `json:"dashboard_url"`
}
type ListInput struct {
	Query  string `json:"query,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type DeleteInput struct {
	ID string `json:"id"`
}

type PageLink struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	MediaType string   `json:"media_type"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	SizeBytes int64    `json:"size_bytes"`
	Revision  int64    `json:"revision"`
	Path      string   `json:"path"`
	URL       string   `json:"url"`
	LANURLs   []string `json:"lan_urls"`
}
type PageOutput struct {
	Page         PageLink `json:"page"`
	DashboardURL string   `json:"dashboard_url"`
}
type ListOutput struct {
	Pages        []PageLink `json:"pages"`
	Total        int        `json:"total"`
	NextOffset   *int       `json:"next_offset"`
	DashboardURL string     `json:"dashboard_url"`
}
type DeleteOutput struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func schema(raw string) any { return json.RawMessage(raw) }

const idSchema = `{"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$","description":"Unique UUID returned by this server, not the display title."}`

func newMCP(store *Store, port int) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "pagehub", Version: version}, &mcp.ServerOptions{
		Instructions: "Pagehub hosts self-contained UTF-8 HTML pages on this computer and its LAN. Submit actual HTML text, never filesystem paths, remote URLs, ZIPs or separate asset files. Inline CSS, JavaScript, SVG and data URLs are supported. External dependencies are not downloaded or hosted. Titles may repeat; the server generates IDs. Use IDs for read/update/patch/delete. For small changes, read_page returns exact source and revision, then patch_page replaces unique old_text strings using expected_revision. A failed edit or stale revision saves nothing; read again before retrying. Changes take effect immediately; users refresh the dashboard/page. Private storage is managed automatically; no need to know its location. Pages never expire and have no count or business size limits. Prefer a returned lan_urls entry for a phone, and the local url for this computer.",
	})
	closed, additive, destructive := false, false, true
	mcp.AddTool(server, &mcp.Tool{
		Name: "create_page", Title: "创建 HTML 页面",
		Description: "Create a NEW self-contained HTML page. Provide a title and the complete UTF-8 HTML document as text with an <html> element. media_type must be text/html. Inline CSS/JS/SVG and data URLs are accepted. No paths, URL imports, Markdown, PDF, ZIP or separate assets. Titles can repeat; each call creates a new server-generated UUID. Returns ID, local URL, LAN URLs and timestamps. Immediately visible on the dashboard after refresh.",
		InputSchema: schema(`{"type":"object","properties":{"title":{"type":"string","minLength":1},"media_type":{"type":"string","const":"text/html"},"html":{"type":"string","minLength":1,"description":"Complete HTML document as UTF-8 text, not a path, URL or base64-encoded file."}},"required":["title","media_type","html"],"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &additive, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in CreateInput) (*mcp.CallToolResult, PageOutput, error) {
		if in.MediaType != "text/html" {
			return nil, PageOutput{}, fmt.Errorf("only text/html is accepted")
		}
		p, err := store.Create(in.Title, in.HTML)
		if err != nil {
			return nil, PageOutput{}, err
		}
		log.Printf("created page %s (%d bytes)", p.ID, p.SizeBytes)
		return nil, PageOutput{Page: linkFor(p, port), DashboardURL: localBase(port) + "/"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_page", Title: "更新 HTML 页面",
		Description: "Update an existing page by UUID, preserving its URL and creation time. Supply title and/or a full replacement html document. When supplying html, also supply media_type=text/html. Optional expected_revision protects against concurrent changes; omitted means unconditional replacement. Use read_page then patch_page for small edits. Changes take effect immediately; refresh the page.",
		InputSchema: schema(`{"type":"object","properties":{"id":` + idSchema + `,"title":{"type":"string","minLength":1},"media_type":{"type":"string","const":"text/html"},"html":{"type":"string","minLength":1},"expected_revision":{"type":"integer","minimum":1}},"required":["id"],"anyOf":[{"required":["title"]},{"required":["html"]}],"dependentRequired":{"html":["media_type"],"media_type":["html"]},"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in UpdateInput) (*mcp.CallToolResult, PageOutput, error) {
		if in.HTML != nil && (in.MediaType == nil || *in.MediaType != "text/html") {
			return nil, PageOutput{}, fmt.Errorf("html replacement requires media_type=text/html")
		}
		p, err := store.UpdateChecked(in.ID, in.Title, in.HTML, in.ExpectedRevision)
		if err != nil {
			return nil, PageOutput{}, err
		}
		log.Printf("updated page %s (%d bytes)", p.ID, p.SizeBytes)
		return nil, PageOutput{Page: linkFor(p, port), DashboardURL: localBase(port) + "/"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_pages", Title: "检索 HTML 页面",
		Description: "List/search deployed pages by title or UUID (case-insensitive). Returns IDs, titles, timestamps, sizes and local/LAN URLs, newest first. All saved pages are available; none are sleeping or expired. Results are paginated for readable tool responses; default limit 100, limit 0 means all. Pagination never limits how many pages may be stored.",
		InputSchema: schema(`{"type":"object","properties":{"query":{"type":"string"},"offset":{"type":"integer","minimum":0,"default":0},"limit":{"type":"integer","minimum":0,"default":100}},"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListOutput, error) {
		pages := store.List(in.Query)
		start := min(in.Offset, len(pages))
		end := len(pages)
		if in.Limit > 0 && in.Limit < end-start {
			end = start + in.Limit
		}
		out := ListOutput{Pages: make([]PageLink, 0, end-start), Total: len(pages), DashboardURL: localBase(port) + "/"}
		bases := lanURLs(port, "")
		for _, p := range pages[start:end] {
			out.Pages = append(out.Pages, linkWithLAN(p, port, bases))
		}
		if end < len(pages) {
			out.NextOffset = &end
		}
		return nil, out, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "delete_page", Title: "删除 HTML 页面",
		Description: "Permanently delete a deployed page by UUID, removing its stored HTML, metadata and in-memory catalog entry. Its URL then returns 404. Only the server-managed copy is deleted; original caller files are untouched. Titles are not identifiers. A page already delivered to a browser cannot be recalled.",
		InputSchema: schema(`{"type":"object","properties":{"id":` + idSchema + `},"required":["id"],"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in DeleteInput) (*mcp.CallToolResult, DeleteOutput, error) {
		if err := store.Delete(in.ID); err != nil {
			return nil, DeleteOutput{}, err
		}
		log.Printf("deleted page %s", in.ID)
		return nil, DeleteOutput{ID: in.ID, Deleted: true}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "read_page", Title: "读取页面源码",
		Description: "Read exact UTF-8 HTML source and its revision by UUID. Omit line bounds to read the whole file. start_line and end_line are 1-based and inclusive; end_line beyond EOF is clamped. Line endings are preserved and no line numbers are inserted into content. No file paths. Use returned revision as expected_revision when patching; narrow ranges reduce tool response size.",
		InputSchema: schema(`{"type":"object","properties":{"id":` + idSchema + `,"start_line":{"type":"integer","minimum":1,"default":1},"end_line":{"type":"integer","minimum":1}},"required":["id"],"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, ReadOutput, error) {
		if in.StartLine == 0 {
			in.StartLine = 1
		}
		p, content, end, total, err := store.Read(in.ID, in.StartLine, in.EndLine)
		if err != nil {
			return nil, ReadOutput{}, err
		}
		return nil, ReadOutput{Page: linkFor(p, port), Content: content, StartLine: in.StartLine, EndLine: end, TotalLines: total}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "patch_page", Title: "局部修改页面",
		Description: "Atomically patch an existing single HTML file by UUID. First read_page, then pass its revision as required expected_revision. Each edit replaces old_text with new_text; old_text must be nonempty and match EXACTLY ONCE (include context if repeated). Edits run sequentially, so later edits see earlier results. new_text may be empty to delete text. Any failed match, invalid final HTML or revision conflict saves nothing. Success increments revision once and preserves URL and creation time. Refresh browser to see changes. No regex, paths, separate assets or fuzzy matches.",
		InputSchema: schema(`{"type":"object","properties":{"id":` + idSchema + `,"expected_revision":{"type":"integer","minimum":1},"edits":{"type":"array","minItems":1,"items":{"type":"object","properties":{"old_text":{"type":"string","minLength":1},"new_text":{"type":"string"}},"required":["old_text","new_text"],"additionalProperties":false}}},"required":["id","expected_revision","edits"],"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &closed},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in PatchInput) (*mcp.CallToolResult, PatchOutput, error) {
		p, err := store.Patch(in.ID, in.ExpectedRevision, in.Edits)
		if err != nil {
			return nil, PatchOutput{}, err
		}
		log.Printf("patched page %s (%d edits, revision %d)", p.ID, len(in.Edits), p.Revision)
		return nil, PatchOutput{Page: linkFor(p, port), AppliedEdits: len(in.Edits), DashboardURL: localBase(port) + "/"}, nil
	})
	return server
}

func localBase(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

func linkFor(p Page, port int) PageLink {
	return linkWithLAN(p, port, lanURLs(port, ""))
}

func linkWithLAN(p Page, port int, bases []string) PageLink {
	path := "/" + p.ID + "/"
	urls := make([]string, 0, len(bases))
	for _, base := range bases {
		urls = append(urls, base+path)
	}
	return PageLink{ID: p.ID, Title: p.Title, MediaType: p.MediaType, CreatedAt: p.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: p.UpdatedAt.Format(time.RFC3339Nano), SizeBytes: p.SizeBytes, Revision: p.Revision, Path: path, URL: localBase(port) + path, LANURLs: urls}
}

func lanURLs(port int, path string) []string {
	urls := make([]string, 0)
	interfaces, err := net.Interfaces()
	if err != nil {
		return urls
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && ip.IsPrivate() {
				urls = append(urls, "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(port))+path)
			}
		}
	}
	return urls
}
