package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	panelapi "sun-panel/api/api_v1/panel"
	panel "sun-panel/integration"
	"sun-panel/models"
	"sun-panel/models/datatype"
)

type mcpDesktop struct {
	Items               []panel.MCPWebsite `json:"items"`
	Panel               map[string]any     `json:"panel"`
	SearchEngine        map[string]any     `json:"searchEngine"`
	PreferencesRevision string             `json:"preferencesRevision"`
}

func (a *Admin) mcpDesktop(ctx context.Context) (mcpDesktop, error) {
	var desktop mcpDesktop
	result, err := a.mcpPanel(ctx, "desktop", nil)
	if err != nil {
		return desktop, err
	}
	data, err := json.Marshal(result)
	if err == nil {
		err = json.Unmarshal(data, &desktop)
	}
	return desktop, err
}

func mcpRouteLinks(config Config) map[string]models.GateHomeLink {
	links := map[string]models.GateHomeLink{}
	for _, route := range sunPanelRouteList(config) {
		title := []rune(route["title"])
		if len(title) > 20 {
			title = title[:20]
		}
		links[route["id"]] = models.GateHomeLink{RouteID: route["id"], Title: string(title), URL: route["url"], LanURL: route["lanUrl"]}
	}
	return links
}

func (a *Admin) mcpPanelTools(server *mcp.Server) {
	type importArgs struct {
		GroupID  int      `json:"groupId"`
		RouteIDs []string `json:"routeIds"`
	}
	addMCPTool(a, server, "sunpanel_import_gatehome_routes", "把 1–100 个已启用的 GateHome 反代项导入绑定账号的指定分组，自动填写默认/内网网址及来源关联，跳过已有网址或关联项。先查询分组和可导入反代列表。", mcpWrite, func(ctx context.Context, args importArgs) (any, error) {
		if len(args.RouteIDs) == 0 || len(args.RouteIDs) > 100 {
			return nil, errors.New("请选择 1–100 个反代项")
		}
		state := a.store.Snapshot()
		links := mcpRouteLinks(state.Config)
		items := []models.ItemIcon{}
		for _, id := range args.RouteIDs {
			link, ok := links[id]
			if !ok {
				return nil, errors.New("反代项已停用或不存在，请刷新反代列表")
			}
			items = append(items, models.ItemIcon{Title: link.Title, Url: link.URL, LanUrl: link.LanURL, ItemIconGroupId: args.GroupID, OpenMethod: 2, GateHome: &link, Icon: datatype.ItemIconIconInfo{ItemType: 3, Text: link.Title}})
		}
		return a.mcpPanelWrite(ctx, "import_routes", map[string]any{"items": items}, state.Revision)
	})
	addMCPTool(a, server, "sunpanel_preview_route_sync", "预览绑定账号中关联网站与当前 GateHome 反代的差异。返回 configRevision、网站 id/updatedAt、字段新旧值及 preserved；preserved=true 表示用户自定义字段会保留。此工具不写入。", mcpRead, func(ctx context.Context, _ mcpEmpty) (any, error) {
		desktop, err := a.mcpDesktop(ctx)
		if err != nil {
			return nil, err
		}
		state := a.store.Snapshot()
		links := mcpRouteLinks(state.Config)
		items := []map[string]any{}
		for _, item := range desktop.Items {
			if item.GateHome == nil {
				continue
			}
			link, ok := links[item.GateHome.RouteID]
			changes := []map[string]any{}
			if ok {
				for _, field := range []struct{ name, current, baseline, next string }{{"title", item.Title, item.GateHome.Title, link.Title}, {"url", item.URL, item.GateHome.URL, link.URL}, {"lanUrl", item.LanURL, item.GateHome.LanURL, link.LanURL}} {
					if field.baseline != field.next {
						changes = append(changes, map[string]any{"field": field.name, "current": field.current, "next": field.next, "preserved": field.current != field.baseline})
					}
				}
			}
			items = append(items, map[string]any{"id": item.ID, "updatedAt": item.UpdatedAt, "title": item.Title, "sourceAvailable": ok, "changes": changes})
		}
		return map[string]any{"configRevision": state.Revision, "items": items}, nil
	})
	type syncArgs struct {
		ConfigRevision int `json:"configRevision"`
		Items          []struct {
			ID        uint      `json:"id"`
			UpdatedAt time.Time `json:"updatedAt"`
		} `json:"items"`
		Confirm bool `json:"confirm"`
	}
	addMCPTool(a, server, "sunpanel_apply_route_sync", "应用已预览的关联更新，保留用户自定义标题和网址。传预览中的 configRevision、网站 id/updatedAt，确认后 confirm=true。停用或删除的源规则不自动删除首页网站。", mcpWrite, func(ctx context.Context, args syncArgs) (any, error) {
		if !args.Confirm || len(args.Items) == 0 || len(args.Items) > 100 {
			return nil, errors.New("请先预览并确认 1–100 个关联更新")
		}
		desktop, err := a.mcpDesktop(ctx)
		if err != nil {
			return nil, err
		}
		links := mcpRouteLinks(a.store.Snapshot().Config)
		rows := []panelapi.GateHomeSyncItem{}
		for _, request := range args.Items {
			found := false
			for _, item := range desktop.Items {
				if item.ID != request.ID || item.GateHome == nil {
					continue
				}
				link, ok := links[item.GateHome.RouteID]
				if !ok {
					return nil, errors.New("关联源已停用或删除，请重新预览")
				}
				rows = append(rows, panelapi.GateHomeSyncItem{ID: request.ID, UpdatedAt: request.UpdatedAt, Route: link})
				found = true
				break
			}
			if !found {
				return nil, errors.New("首页项目已变化，请重新预览")
			}
		}
		return a.mcpPanelWrite(ctx, "sync_routes", map[string]any{"items": rows}, args.ConfigRevision)
	})
	addMCPTool(a, server, "sunpanel_set_preferences", "替换绑定账号的桌面和搜索偏好；先读取首页完整 panel/searchEngine 及 preferencesRevision，保留其他偏好字段。", mcpWrite, func(ctx context.Context, args panel.MCPPreferences) (any, error) {
		desktop, err := a.mcpDesktop(ctx)
		if err != nil {
			return nil, err
		}
		for _, field := range []struct {
			value *map[string]any
			old   map[string]any
		}{{&args.Panel, desktop.Panel}, {&args.SearchEngine, desktop.SearchEngine}} {
			restored, err := mcpRestoreRedactions(*field.value, field.old)
			if err != nil {
				return nil, err
			}
			*field.value, _ = restored.(map[string]any)
		}
		return a.mcpPanelWrite(ctx, "set_preferences", args, -1)
	})
}
