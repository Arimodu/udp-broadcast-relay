package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Arimodu/udp-broadcast-relay/internal/auth"
	"github.com/Arimodu/udp-broadcast-relay/internal/database"
	"github.com/Arimodu/udp-broadcast-relay/internal/updater"
	"github.com/Arimodu/udp-broadcast-relay/web"
)

type WebUI struct {
	port    int
	db      *database.DB
	hub     *Hub
	log     *slog.Logger
	auth    *auth.Service
	sess    *SessionStore
	updater *updater.Checker // nil when update checking is disabled
	server  *http.Server
}

func NewWebUI(port int, db *database.DB, hub *Hub, log *slog.Logger, checker *updater.Checker) *WebUI {
	return &WebUI{
		port:    port,
		db:      db,
		hub:     hub,
		log:     log,
		auth:    auth.NewService(db),
		sess:    NewSessionStore(db),
		updater: checker,
	}
}

func (w *WebUI) Serve(ctx context.Context) error {
	mux := http.NewServeMux()

	// Static files
	staticFS, err := fs.Sub(web.Assets, "static")
	if err != nil {
		return fmt.Errorf("static fs: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Login page
	mux.HandleFunc("GET /login", w.handleLoginPage)
	mux.HandleFunc("POST /api/auth/login", w.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", w.handleLogout)

	// Protected API routes
	mux.HandleFunc("GET /api/auth/me", w.requireAuth(w.handleMe))
	mux.HandleFunc("GET /api/clients", w.requireAuth(w.handleGetClients))

	mux.HandleFunc("GET /api/rules", w.requireAuth(w.handleGetRules))
	mux.HandleFunc("POST /api/rules", w.requireAuth(w.handleCreateRule))
	mux.HandleFunc("PUT /api/rules/{id}", w.requireAuth(w.handleUpdateRule))
	mux.HandleFunc("DELETE /api/rules/{id}", w.requireAuth(w.handleDeleteRule))
	mux.HandleFunc("PUT /api/rules/{id}/toggle", w.requireAuth(w.handleToggleRule))

	// Client-rule assignment
	mux.HandleFunc("GET /api/keys/{id}/rules", w.requireAuth(w.handleGetKeyRules))
	mux.HandleFunc("POST /api/keys/{id}/rules/{rule_id}", w.requireAuth(w.handleAssignRule))
	mux.HandleFunc("DELETE /api/keys/{id}/rules/{rule_id}", w.requireAuth(w.handleUnassignRule))

	mux.HandleFunc("GET /api/packets", w.requireAuth(w.handleGetPackets))
	mux.HandleFunc("GET /api/monitor", w.requireAuth(w.handleGetMonitor))

	mux.HandleFunc("GET /api/keys", w.requireAuth(w.handleGetKeys))
	mux.HandleFunc("POST /api/keys", w.requireAuth(w.handleCreateKey))
	mux.HandleFunc("DELETE /api/keys/{id}", w.requireAuth(w.handleDeleteKey))
	mux.HandleFunc("PUT /api/keys/{id}/revoke", w.requireAuth(w.handleRevokeKey))

	mux.HandleFunc("POST /api/settings/password", w.requireAuth(w.handleChangePassword))

	// Update routes
	mux.HandleFunc("GET /api/update/status", w.requireAuth(w.handleUpdateStatus))
	mux.HandleFunc("POST /api/update/check", w.requireAuth(w.handleUpdateCheck))
	mux.HandleFunc("POST /api/update/apply", w.requireAuth(w.handleUpdateApply))

	// SPA catch-all (serve index.html for all non-API, non-static routes)
	mux.HandleFunc("GET /", w.requireAuthPage(w.handleSPA))

	w.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", w.port),
		Handler: mux,
	}

	w.log.Info("webui started", "port", w.port)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		w.server.Shutdown(shutdownCtx)
	}()

	if err := w.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Auth middleware for API routes
func (w *WebUI) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("ubr_session")
		if err != nil {
			jsonError(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		userID, ok := w.sess.Get(cookie.Value)
		if !ok {
			jsonError(rw, "session expired", http.StatusUnauthorized)
			return
		}

		user, err := w.db.GetUserByID(userID)
		if err != nil || user == nil || !user.IsActive {
			jsonError(rw, "unauthorized", http.StatusUnauthorized)
			return
		}

		r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, user))
		handler(rw, r)
	}
}

// Auth middleware for page routes (redirects to /login)
func (w *WebUI) requireAuthPage(handler http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("ubr_session")
		if err != nil {
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}

		userID, ok := w.sess.Get(cookie.Value)
		if !ok {
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}

		user, err := w.db.GetUserByID(userID)
		if err != nil || user == nil || !user.IsActive {
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}

		r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, user))
		handler(rw, r)
	}
}

type contextKey string

const ctxUserKey contextKey = "user"

func getUser(r *http.Request) *database.User {
	user, _ := r.Context().Value(ctxUserKey).(*database.User)
	return user
}

// Page handlers
func (w *WebUI) handleLoginPage(rw http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(web.Assets, "templates/login.html")
	if err != nil {
		http.Error(rw, "template error", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(rw, nil)
}

func (w *WebUI) handleSPA(rw http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(web.Assets, "templates/index.html")
	if err != nil {
		http.Error(rw, "template error", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(rw, getUser(r))
}

// Auth API handlers
func (w *WebUI) handleLogin(rw http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "invalid request", http.StatusBadRequest)
		return
	}

	user, token, err := w.auth.AuthenticatePassword(req.Username, req.Password)
	if err != nil {
		w.log.Error("login error", "error", err)
		jsonError(rw, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		jsonError(rw, "invalid credentials", http.StatusUnauthorized)
		return
	}

	w.sess.Set(token, user.ID)

	http.SetCookie(rw, &http.Cookie{
		Name:     "ubr_session",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	jsonResponse(rw, map[string]interface{}{
		"success":  true,
		"username": user.Username,
		"is_admin": user.IsAdmin,
	})
}

func (w *WebUI) handleLogout(rw http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("ubr_session")
	if err == nil {
		w.sess.Delete(cookie.Value)
	}

	http.SetCookie(rw, &http.Cookie{
		Name:   "ubr_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	jsonResponse(rw, map[string]bool{"success": true})
}

func (w *WebUI) handleMe(rw http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	jsonResponse(rw, map[string]interface{}{
		"id":       user.ID,
		"username": user.Username,
		"is_admin": user.IsAdmin,
	})
}

// Client handlers
func (w *WebUI) handleGetClients(rw http.ResponseWriter, r *http.Request) {
	clients := w.hub.GetConnectedClients()
	jsonResponse(rw, clients)
}

// Rule handlers
func (w *WebUI) handleGetRules(rw http.ResponseWriter, r *http.Request) {
	rules, err := w.db.ListRules()
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}
	jsonResponse(rw, rules)
}

func (w *WebUI) handleCreateRule(rw http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		ListenPort    int    `json:"listen_port"`
		ListenIP      string `json:"listen_ip"`
		DestBroadcast string `json:"dest_broadcast"`
		Direction     string `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "invalid request", http.StatusBadRequest)
		return
	}

	if req.ListenIP == "" {
		req.ListenIP = "0.0.0.0"
	}
	if req.DestBroadcast == "" {
		req.DestBroadcast = "255.255.255.255"
	}
	if req.Direction == "" {
		req.Direction = "server_to_client"
	}

	rule, err := w.db.CreateRule(req.Name, req.ListenPort, req.ListenIP, req.DestBroadcast, req.Direction)
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	w.log.Info("rule created", "name", rule.Name, "port", rule.ListenPort)
	jsonResponse(rw, rule)
}

func (w *WebUI) handleUpdateRule(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Name          string `json:"name"`
		ListenPort    int    `json:"listen_port"`
		ListenIP      string `json:"listen_ip"`
		DestBroadcast string `json:"dest_broadcast"`
		Direction     string `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "invalid request", http.StatusBadRequest)
		return
	}

	if err := w.db.UpdateRule(id, req.Name, req.ListenPort, req.ListenIP, req.DestBroadcast, req.Direction); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

func (w *WebUI) handleDeleteRule(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid id", http.StatusBadRequest)
		return
	}

	if err := w.db.DeleteRule(id); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

func (w *WebUI) handleToggleRule(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid id", http.StatusBadRequest)
		return
	}

	if err := w.db.ToggleRule(id); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

// Packet log handler
func (w *WebUI) handleGetPackets(rw http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, _ = strconv.Atoi(v)
	}

	var ruleID *int64
	if v := r.URL.Query().Get("rule_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			ruleID = &id
		}
	}

	entries, total, err := w.db.GetPacketLog(limit, offset, ruleID)
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]interface{}{
		"entries": entries,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// Monitor handler
func (w *WebUI) handleGetMonitor(rw http.ResponseWriter, r *http.Request) {
	observations, err := w.db.GetBroadcastObservations()
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, observations)
}

// API key handlers
func (w *WebUI) handleGetKeys(rw http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	keys, err := w.db.ListAPIKeysByUser(user.ID)
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	// Mask keys in the response (only show first 8 chars + "...")
	type maskedKey struct {
		ID         int64      `json:"id"`
		Name       string     `json:"name"`
		KeyPreview string     `json:"key_preview"`
		CreatedAt  time.Time  `json:"created_at"`
		LastUsedAt *time.Time `json:"last_used_at"`
		IsRevoked  bool       `json:"is_revoked"`
	}

	var masked []maskedKey
	for _, k := range keys {
		preview := k.Key
		if len(preview) > 12 {
			preview = preview[:12] + "..."
		}
		masked = append(masked, maskedKey{
			ID:         k.ID,
			Name:       k.Name,
			KeyPreview: preview,
			CreatedAt:  k.CreatedAt,
			LastUsedAt: k.LastUsedAt,
			IsRevoked:  k.IsRevoked,
		})
	}

	jsonResponse(rw, masked)
}

func (w *WebUI) handleCreateKey(rw http.ResponseWriter, r *http.Request) {
	user := getUser(r)

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "invalid request", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		jsonError(rw, "name is required", http.StatusBadRequest)
		return
	}

	key := auth.GenerateAPIKey()
	ak, err := w.db.CreateAPIKey(user.ID, key, req.Name)
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	// Return the full key (only time it's shown)
	jsonResponse(rw, map[string]interface{}{
		"id":         ak.ID,
		"key":        ak.Key,
		"name":       ak.Name,
		"created_at": ak.CreatedAt,
	})
}

func (w *WebUI) handleDeleteKey(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid id", http.StatusBadRequest)
		return
	}

	if err := w.db.DeleteAPIKey(id); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

func (w *WebUI) handleRevokeKey(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid id", http.StatusBadRequest)
		return
	}

	if err := w.db.RevokeAPIKey(id); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

// Settings handlers
func (w *WebUI) handleChangePassword(rw http.ResponseWriter, r *http.Request) {
	user := getUser(r)

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(rw, "invalid request", http.StatusBadRequest)
		return
	}

	// Verify old password
	verified, _, err := w.auth.AuthenticatePassword(user.Username, req.OldPassword)
	if err != nil || verified == nil {
		jsonError(rw, "incorrect current password", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		jsonError(rw, "internal error", http.StatusInternalServerError)
		return
	}

	if err := w.db.UpdateUserPassword(user.ID, hash); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

// Rule-client assignment handlers
func (w *WebUI) handleGetKeyRules(rw http.ResponseWriter, r *http.Request) {
	keyID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid id", http.StatusBadRequest)
		return
	}

	rules, err := w.db.GetRulesForClient(keyID)
	if err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	jsonResponse(rw, rules)
}

func (w *WebUI) handleAssignRule(rw http.ResponseWriter, r *http.Request) {
	keyID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid key id", http.StatusBadRequest)
		return
	}
	ruleID, err := strconv.ParseInt(r.PathValue("rule_id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid rule id", http.StatusBadRequest)
		return
	}

	if err := w.db.AssignRuleToClient(keyID, ruleID); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	// Push updated rules to client if connected
	if rules, err := w.db.GetRulesForClient(keyID); err == nil {
		w.hub.PushRuleUpdate(keyID, rules)
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

func (w *WebUI) handleUnassignRule(rw http.ResponseWriter, r *http.Request) {
	keyID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid key id", http.StatusBadRequest)
		return
	}
	ruleID, err := strconv.ParseInt(r.PathValue("rule_id"), 10, 64)
	if err != nil {
		jsonError(rw, "invalid rule id", http.StatusBadRequest)
		return
	}

	if err := w.db.UnassignRuleFromClient(keyID, ruleID); err != nil {
		jsonError(rw, "database error", http.StatusInternalServerError)
		return
	}

	// Push updated rules to client if connected
	if rules, err := w.db.GetRulesForClient(keyID); err == nil {
		w.hub.PushRuleUpdate(keyID, rules)
	}

	jsonResponse(rw, map[string]bool{"success": true})
}

// Update handlers

type updateStatusResponse struct {
	Enabled          bool       `json:"enabled"`
	CurrentVersion   string     `json:"current_version"`
	LatestVersion    string     `json:"latest_version,omitempty"`
	LatestTag        string     `json:"latest_tag,omitempty"`
	UpdateAvailable  bool       `json:"update_available"`
	AssetAvailable   bool       `json:"asset_available"`
	LastChecked      *time.Time `json:"last_checked,omitempty"`
}

func (w *WebUI) buildUpdateStatus() updateStatusResponse {
	if w.updater == nil {
		return updateStatusResponse{Enabled: false}
	}
	resp := updateStatusResponse{
		Enabled:        true,
		CurrentVersion: w.updater.CurrentVersion(),
	}
	available, latest, checked := w.updater.Status()
	if !checked.IsZero() {
		resp.LastChecked = &checked
	}
	if latest != nil {
		resp.LatestVersion = latest.Version
		resp.LatestTag = latest.Tag
		resp.AssetAvailable = latest.AssetURL != ""
	}
	resp.UpdateAvailable = available
	return resp
}

func (w *WebUI) handleUpdateStatus(rw http.ResponseWriter, r *http.Request) {
	jsonResponse(rw, w.buildUpdateStatus())
}

func (w *WebUI) handleUpdateCheck(rw http.ResponseWriter, r *http.Request) {
	if w.updater == nil {
		jsonError(rw, "update checking is disabled (set check_updates = true in config)", http.StatusServiceUnavailable)
		return
	}
	if err := w.updater.Check(); err != nil {
		w.log.Warn("manual update check failed", "error", err)
		jsonError(rw, fmt.Sprintf("update check failed: %v", err), http.StatusBadGateway)
		return
	}
	jsonResponse(rw, w.buildUpdateStatus())
}

func (w *WebUI) handleUpdateApply(rw http.ResponseWriter, r *http.Request) {
	if w.updater == nil {
		jsonError(rw, "update checking is disabled", http.StatusServiceUnavailable)
		return
	}
	available, _, _ := w.updater.Status()
	if !available {
		jsonError(rw, "no update available", http.StatusConflict)
		return
	}
	msg, err := w.updater.Apply()
	if err != nil {
		w.log.Error("applying update failed", "error", err)
		jsonError(rw, fmt.Sprintf("update failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.log.Info("update applied", "message", msg)
	jsonResponse(rw, map[string]string{"message": msg})
}

// JSON helpers
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
