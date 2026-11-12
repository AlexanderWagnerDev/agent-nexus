package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

var mcpProtocolVersion = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

func mcpTools() []map[string]any {
	return []map[string]any{
		{
			"name": "list_hosts", "description": "List hosts visible to the token owner, including online state and agent inventory.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name": "list_sessions", "description": "List session metadata visible to the token owner.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"host_id": map[string]any{"type": "string"},
				"agent":   map[string]any{"type": "string"},
				"query":   map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
				"offset":  map[string]any{"type": "integer", "minimum": 0},
			}},
		},
		{
			"name": "search_transcripts", "description": "Search transcripts on online hosts the token may read.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string"},
			}, "required": []string{"query"}},
		},
		{
			"name": "get_transcript_page", "description": "Read a bounded transcript page from an online host.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"session_id": map[string]any{"type": "string"},
				"offset":     map[string]any{"type": "integer", "minimum": 0},
				"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
			}, "required": []string{"session_id"}},
		},
		{
			"name": "get_git_status", "description": "Read the working-tree status and diff for a session without writing to Git.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"session_id": map[string]any{"type": "string"},
			}, "required": []string{"session_id"}},
		},
	}
}

func mcpToolText(value any, isError bool) map[string]any {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(raw)}},
		"isError": isError,
	}
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="AgentNexus MCP"`)
		writeError(w, http.StatusUnauthorized, "bearer token required")
		return
	}
	user, tokenID, ok := s.store.APITokenUser(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or revoked token")
		return
	}
	if !s.mcpLimiter.allow(tokenID) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	var req mcpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "invalid JSON"}})
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		writeJSON(w, http.StatusBadRequest, mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: -32600, Message: "invalid JSON-RPC request"}})
		return
	}
	if req.Method == "notifications/initialized" || req.Method == "notifications/cancelled" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply := func(result any, err *mcpError) {
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: err})
	}
	switch req.Method {
	case "initialize":
		reply(map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "AgentNexus", "version": Version},
		}, nil)
	case "ping":
		reply(map[string]any{}, nil)
	case "tools/list":
		reply(map[string]any{"tools": mcpTools()}, nil)
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			reply(nil, &mcpError{Code: -32602, Message: "tools/call requires name"})
			return
		}
		result, err := s.callMCPTool(r.Context(), user, params.Name, params.Arguments)
		if err != nil {
			s.audit(user.Username, "mcp.tool_failed", map[string]string{"tool": params.Name, "token_id": tokenID})
			reply(mcpToolText(map[string]string{"error": err.Error()}, true), nil)
			return
		}
		s.audit(user.Username, "mcp.tool_called", map[string]string{"tool": params.Name, "token_id": tokenID})
		reply(mcpToolText(result, false), nil)
	default:
		reply(nil, &mcpError{Code: -32601, Message: "method not found"})
	}
}

func (s *Server) callMCPTool(ctx context.Context, u User, name string, raw json.RawMessage) (any, error) {
	all := isAdminRole(u.Role)
	switch name {
	case "list_hosts":
		hosts, err := s.store.ListHosts(u.ID, all)
		if err != nil {
			return nil, err
		}
		return map[string]any{"hosts": hosts}, nil
	case "list_sessions":
		var args struct {
			HostID string `json:"host_id"`
			Agent  string `json:"agent"`
			Query  string `json:"query"`
			Limit  int    `json:"limit"`
			Offset int    `json:"offset"`
		}
		_ = json.Unmarshal(raw, &args)
		if args.Limit <= 0 || args.Limit > 200 {
			args.Limit = 50
		}
		filter := SessionFilter{HostID: args.HostID, Agent: args.Agent, Query: args.Query, Limit: args.Limit, Offset: args.Offset, Sort: "updated"}
		sessions, err := s.store.ListSessionsFiltered(u.ID, filter, all)
		if err != nil {
			return nil, err
		}
		if sessions == nil {
			sessions = []protocol.SessionView{}
		}
		total, _ := s.store.CountSessionsFiltered(u.ID, filter, all)
		return map[string]any{"sessions": sessions, "total": total}, nil
	case "search_transcripts":
		var args struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(raw, &args)
		query := strings.TrimSpace(args.Query)
		if len(query) < 2 || len(query) > 200 {
			return nil, fmt.Errorf("query must be 2-200 characters")
		}
		return s.mcpSearch(ctx, u, query)
	case "get_transcript_page":
		var args struct {
			SessionID string `json:"session_id"`
			Offset    int    `json:"offset"`
			Limit     int    `json:"limit"`
		}
		_ = json.Unmarshal(raw, &args)
		return s.mcpTranscript(ctx, u, args.SessionID, args.Offset, args.Limit)
	case "get_git_status":
		var args struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(raw, &args)
		return s.mcpGit(ctx, u, args.SessionID)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *Server) mcpSession(u User, id string, need string) (protocol.SessionView, error) {
	sess, err := s.store.SessionByID(u.ID, id, isAdminRole(u.Role))
	if err != nil {
		return sess, fmt.Errorf("session not found")
	}
	if !s.sessionPermission(u, sess, need) {
		return sess, fmt.Errorf("%s permission required", need)
	}
	return sess, nil
}

func (s *Server) mcpTranscript(ctx context.Context, u User, sessionID string, offset, limit int) (any, error) {
	sess, err := s.mcpSession(u, sessionID, "read_transcript")
	if err != nil {
		return nil, err
	}
	if !s.hostOnline(sess.HostID) {
		return nil, fmt.Errorf("host is offline")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params, _ := json.Marshal(map[string]any{"cwd": sess.Cwd, "native_id": sess.NativeID, "agent": sess.Agent, "offset": offset, "limit": limit})
	raw, err := s.hostRequest(cctx, sess.HostID, "transcript", params)
	if err != nil {
		return nil, err
	}
	var transcript protocol.Transcript
	if err := json.Unmarshal(raw, &transcript); err != nil {
		return nil, fmt.Errorf("invalid host response")
	}
	return transcript, nil
}

func (s *Server) mcpGit(ctx context.Context, u User, sessionID string) (any, error) {
	sess, err := s.mcpSession(u, sessionID, "read_transcript")
	if err != nil {
		return nil, err
	}
	if !s.hostOnline(sess.HostID) {
		return nil, fmt.Errorf("host is offline")
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params, _ := json.Marshal(map[string]string{"cwd": sess.Cwd})
	raw, err := s.hostRequest(cctx, sess.HostID, "git", params)
	if err != nil {
		return nil, err
	}
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		return nil, fmt.Errorf("invalid host response")
	}
	return view, nil
}

func (s *Server) mcpSearch(ctx context.Context, u User, query string) (any, error) {
	hosts, err := s.store.ListHosts(u.ID, isAdminRole(u.Role))
	if err != nil {
		return nil, err
	}
	type result struct {
		hits     []protocol.SearchHit
		hostName string
		hostID   string
	}
	ch := make(chan result, len(hosts))
	online := 0
	for _, host := range hosts {
		if !s.hostPermission(u, host.ID, "read_transcript") || !s.hostOnline(host.ID) {
			continue
		}
		online++
		go func(hostID, hostName string) {
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			params, _ := json.Marshal(map[string]any{"query": query, "limit": 50})
			raw, err := s.hostRequest(cctx, hostID, "search", params)
			if err != nil {
				ch <- result{hostName: hostName, hostID: hostID}
				return
			}
			var hits []protocol.SearchHit
			_ = json.Unmarshal(raw, &hits)
			ch <- result{hits: hits, hostName: hostName, hostID: hostID}
		}(host.ID, host.Name)
	}
	all := []protocol.SearchHit{}
	notSearched := []string{}
	for i := 0; i < online; i++ {
		res := <-ch
		if len(res.hits) == 0 {
			notSearched = append(notSearched, res.hostName)
			continue
		}
		for _, hit := range res.hits {
			hit.HostID = res.hostID
			hit.HostName = res.hostName
			hit.Title = displayTitle(hit.Title)
			if id, err := s.store.SessionIDForNative(res.hostID, hit.Agent, hit.NativeID); err == nil {
				hit.SessionID = id
			}
			all = append(all, hit)
			if len(all) >= 100 {
				break
			}
		}
	}
	return map[string]any{"results": all, "not_searched": notSearched}, nil
}
