package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"support-ticket-system/backend/internal/auth"
	"support-ticket-system/backend/internal/config"
	"support-ticket-system/backend/internal/realtime"
	"support-ticket-system/backend/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type App struct {
	Cfg     config.Config
	DB      *pgxpool.Pool
	Hub     *realtime.Hub
	Storage storage.Storage
}

func NewApp(cfg config.Config, pool *pgxpool.Pool, hub *realtime.Hub, store storage.Storage) *App {
	return &App{Cfg: cfg, DB: pool, Hub: hub, Storage: store}
}

func (a *App) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.Handle("GET /api/auth/me", a.authRequired(http.HandlerFunc(a.me)))
	mux.Handle("GET /api/tickets", a.authRequired(http.HandlerFunc(a.listTickets)))
	mux.Handle("POST /api/tickets", a.authRequired(http.HandlerFunc(a.createTicket)))
	mux.Handle("GET /api/tickets/{id}", a.authRequired(http.HandlerFunc(a.getTicket)))
	mux.Handle("PATCH /api/tickets/{id}/status", a.requireRole("support", http.HandlerFunc(a.updateTicketStatus)))
	mux.Handle("GET /api/files/{id}", a.authRequired(http.HandlerFunc(a.downloadFile)))
	mux.Handle("GET /api/kpi", a.requireRole("support", http.HandlerFunc(a.kpi)))
	mux.Handle("POST /api/users", a.requireRole("support", http.HandlerFunc(a.createUser)))
	mux.Handle("GET /api/users", a.requireRole("support", http.HandlerFunc(a.listUsers)))
	mux.HandleFunc("GET /ws", a.websocket)

	return a.cors(a.logging(mux))
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
}

func (a *App) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		user, err := auth.ParseToken(a.Cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
	})
}

func (a *App) requireRole(role string, next http.Handler) http.Handler {
	return a.authRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFromContext(r.Context())
		if user.Role != role {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &payload) {
		return
	}
	payload.Email = strings.ToLower(strings.TrimSpace(payload.Email))
	var user auth.User
	var hash string
	err := a.DB.QueryRow(r.Context(), `SELECT id, full_name, email, role::text, password_hash FROM users WHERE lower(email)=lower($1) AND active=TRUE`, payload.Email).
		Scan(&user.ID, &user.FullName, &user.Email, &user.Role, &hash)
	if err != nil || !auth.CheckPassword(hash, payload.Password) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, err := auth.CreateToken(a.Cfg.JWTSecret, time.Duration(a.Cfg.JWTTTLHours)*time.Hour, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	var fullName, email, role string
	if err := a.DB.QueryRow(r.Context(), `SELECT full_name, email, role::text FROM users WHERE id=$1 AND active=TRUE`, user.ID).Scan(&fullName, &email, &role); err != nil {
		writeError(w, http.StatusUnauthorized, "user is not active")
		return
	}
	user.FullName, user.Email, user.Role = fullName, email, role
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (a *App) listTickets(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	mine := r.URL.Query().Get("mine") == "true"

	conditions := []string{"1=1"}
	args := []any{}
	arg := 1
	if user.Role == "salesperson" || mine {
		conditions = append(conditions, fmt.Sprintf("t.created_by=$%d", arg))
		args = append(args, user.ID)
		arg++
	}
	if status != "" && status != "all" {
		conditions = append(conditions, fmt.Sprintf("t.status=$%d", arg))
		args = append(args, status)
		arg++
	}
	query := fmt.Sprintf(`
		SELECT t.id, t.ticket_code, t.title, t.description, t.priority::text, t.status::text,
		       t.created_by, u.full_name, u.email, t.assigned_to, t.resolution_message,
		       t.resolved_at, t.created_at, t.updated_at,
		       COUNT(f.id) AS file_count
		FROM tickets t
		JOIN users u ON u.id=t.created_by
		LEFT JOIN ticket_files f ON f.ticket_id=t.id
		WHERE %s
		GROUP BY t.id, u.full_name, u.email
		ORDER BY CASE t.priority WHEN 'urgent' THEN 1 WHEN 'high' THEN 2 WHEN 'normal' THEN 3 ELSE 4 END,
		         t.created_at DESC`, strings.Join(conditions, " AND "))

	rows, err := a.DB.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tickets")
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, code, title, desc, priority, status, createdBy, creatorName, creatorEmail string
		var assignedTo *string
		var resolution *string
		var resolvedAt *time.Time
		var createdAt, updatedAt time.Time
		var fileCount int
		if err := rows.Scan(&id, &code, &title, &desc, &priority, &status, &createdBy, &creatorName, &creatorEmail,
			&assignedTo, &resolution, &resolvedAt, &createdAt, &updatedAt, &fileCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read tickets")
			return
		}
		result = append(result, map[string]any{
			"id": id, "ticketCode": code, "title": title, "description": desc, "priority": priority,
			"status": status, "createdBy": createdBy, "creatorName": creatorName, "creatorEmail": creatorEmail,
			"assignedTo": assignedTo, "resolutionMessage": resolution, "resolvedAt": resolvedAt,
			"createdAt": createdAt, "updatedAt": updatedAt, "fileCount": fileCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": result})
}

func (a *App) createTicket(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	if user.Role != "salesperson" && user.Role != "support" {
		writeError(w, http.StatusForbidden, "invalid role")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.Cfg.MaxUploadBytes+2*1024*1024)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	priority := strings.TrimSpace(r.FormValue("priority"))
	if priority == "" {
		priority = "normal"
	}
	if title == "" || description == "" {
		writeError(w, http.StatusBadRequest, "title and description are required")
		return
	}
	if !validPriority(priority) {
		writeError(w, http.StatusBadRequest, "invalid priority")
		return
	}

	files := r.MultipartForm.File["evidence"]
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	var id, seq int64
	var ticketID string
	ticketUUID := uuid.New()
	if err := tx.QueryRow(ctx, `INSERT INTO tickets(id, title, description, priority, status, created_by) VALUES($1,$2,$3,$4,'open',$5) RETURNING id, ticket_seq`, ticketUUID, title, description, priority, user.ID).Scan(&ticketID, &seq); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}
	ticketCode := fmt.Sprintf("TCK-%s-%06d", time.Now().Format("200601"), seq)
	if _, err := tx.Exec(ctx, `UPDATE tickets SET ticket_code=$1 WHERE id=$2`, ticketCode, ticketID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to number ticket")
		return
	}

	storedKeys := make([]string, 0, len(files))
	for _, header := range files {
		if header.Size > a.Cfg.MaxUploadBytes {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file %s exceeds the upload limit", header.Filename))
			return
		}
		if !allowedContentType(header.Header.Get("Content-Type"), header.Filename) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("file type is not allowed: %s", header.Filename))
			return
		}
		key := filepath.ToSlash(filepath.Join("tickets", ticketID, uuid.NewString()+"-"+safeFileName(header.Filename)))
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read evidence file")
			return
		}
		err = a.Storage.Put(ctx, key, file, header.Header.Get("Content-Type"), header.Size)
		file.Close()
		if err != nil {
			for _, stored := range storedKeys {
				_ = a.Storage.Delete(context.Background(), stored)
			}
			writeError(w, http.StatusInternalServerError, "could not store evidence")
			return
		}
		storedKeys = append(storedKeys, key)
		if _, err := tx.Exec(ctx, `INSERT INTO ticket_files(ticket_id, original_name, storage_key, content_type, size_bytes, uploaded_by) VALUES($1,$2,$3,$4,$5,$6)`,
			ticketID, header.Filename, key, header.Header.Get("Content-Type"), header.Size, user.ID); err != nil {
			for _, stored := range storedKeys {
				_ = a.Storage.Delete(context.Background(), stored)
			}
			writeError(w, http.StatusInternalServerError, "could not save evidence metadata")
			return
		}
	}

	_, err = tx.Exec(ctx, `INSERT INTO ticket_events(ticket_id, actor_id, event_type, payload) VALUES($1,$2,'ticket.created',jsonb_build_object('ticketCode',$3,'title',$4))`, ticketID, user.ID, ticketCode, title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not record ticket event")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		for _, stored := range storedKeys {
			_ = a.Storage.Delete(context.Background(), stored)
		}
		writeError(w, http.StatusInternalServerError, "could not commit ticket")
		return
	}

	response, err := a.ticketByID(ctx, ticketID, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ticket created but could not be returned")
		return
	}
	a.Hub.Broadcast(realtime.Event{Type: "ticket.created", Entity: "ticket", ID: ticketID, Data: response})
	writeJSON(w, http.StatusCreated, response)
}

func (a *App) getTicket(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	id := r.PathValue("id")
	ticket, err := a.ticketByID(r.Context(), id, user)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ticket")
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (a *App) ticketByID(ctx context.Context, id string, viewer auth.User) (map[string]any, error) {
	var creatorID string
	var assignedTo *string
	var ticket map[string]any
	var resolvedAt *time.Time
	var resolution *string
	var createdAt, updatedAt time.Time
	var code, title, description, priority, status, creatorName, creatorEmail string
	var fileCount int
	err := a.DB.QueryRow(ctx, `
		SELECT t.id, t.ticket_code, t.title, t.description, t.priority::text, t.status::text,
		       t.created_by, u.full_name, u.email, t.assigned_to, t.resolution_message,
		       t.resolved_at, t.created_at, t.updated_at, COUNT(f.id)
		FROM tickets t JOIN users u ON u.id=t.created_by
		LEFT JOIN ticket_files f ON f.ticket_id=t.id
		WHERE t.id=$1
		GROUP BY t.id, u.full_name, u.email`, id).
		Scan(&id, &code, &title, &description, &priority, &status, &creatorID, &creatorName, &creatorEmail,
			&assignedTo, &resolution, &resolvedAt, &createdAt, &updatedAt, &fileCount)
	if err != nil {
		return nil, err
	}
	if viewer.Role == "salesperson" && viewer.ID != creatorID {
		return nil, pgx.ErrNoRows
	}

	rows, err := a.DB.Query(ctx, `SELECT id, original_name, content_type, size_bytes, created_at FROM ticket_files WHERE ticket_id=$1 ORDER BY created_at ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]map[string]any, 0, fileCount)
	for rows.Next() {
		var fid, name, ct string
		var size int64
		var created time.Time
		if err := rows.Scan(&fid, &name, &ct, &size, &created); err != nil {
			return nil, err
		}
		files = append(files, map[string]any{"id": fid, "name": name, "contentType": ct, "size": size, "createdAt": created})
	}
	return map[string]any{
		"id": id, "ticketCode": code, "title": title, "description": description, "priority": priority,
		"status": status, "createdBy": creatorID, "creatorName": creatorName, "creatorEmail": creatorEmail,
		"assignedTo": assignedTo, "resolutionMessage": resolution, "resolvedAt": resolvedAt,
		"createdAt": createdAt, "updatedAt": updatedAt, "fileCount": fileCount, "files": files,
	}, nil
}

func (a *App) updateTicketStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	id := r.PathValue("id")
	var payload struct {
		Status            string  `json:"status"`
		ResolutionMessage *string `json:"resolutionMessage"`
	}
	if !readJSON(w, r, &payload) {
		return
	}
	payload.Status = strings.TrimSpace(payload.Status)
	if !validStatus(payload.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if (payload.Status == "resolved" || payload.Status == "rejected") && strings.TrimSpace(deref(payload.ResolutionMessage)) == "" {
		writeError(w, http.StatusBadRequest, "resolution message is mandatory for resolved/rejected tickets")
		return
	}

	var creatorID string
	if err := a.DB.QueryRow(r.Context(), `SELECT created_by FROM tickets WHERE id=$1`, id).Scan(&creatorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "ticket not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not find ticket")
		}
		return
	}
	var resolution any = nil
	if payload.ResolutionMessage != nil {
		resolution = strings.TrimSpace(*payload.ResolutionMessage)
	}
	var resolvedAt any = nil
	if payload.Status == "resolved" || payload.Status == "rejected" {
		resolvedAt = time.Now().UTC()
	}
	_, err := a.DB.Exec(r.Context(), `UPDATE tickets SET status=$1, resolution_message=$2, resolved_at=$3, updated_at=NOW() WHERE id=$4`, payload.Status, resolution, resolvedAt, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update ticket")
		return
	}
	_, _ = a.DB.Exec(r.Context(), `INSERT INTO ticket_events(ticket_id, actor_id, event_type, payload) VALUES($1,$2,'ticket.status_changed',jsonb_build_object('status',$3,'resolutionMessage',$4))`, id, user.ID, payload.Status, resolution)
	viewer := auth.User{ID: creatorID, Role: "salesperson"}
	ticket, err := a.ticketByID(r.Context(), id, viewer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload ticket")
		return
	}
	a.Hub.Broadcast(realtime.Event{Type: "ticket.updated", Entity: "ticket", ID: id, Data: ticket})
	writeJSON(w, http.StatusOK, ticket)
}

func (a *App) downloadFile(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	fileID := r.PathValue("id")
	var ticketID, key, name, contentType string
	var size int64
	if err := a.DB.QueryRow(r.Context(), `SELECT ticket_id, storage_key, original_name, content_type, size_bytes FROM ticket_files WHERE id=$1`, fileID).Scan(&ticketID, &key, &name, &contentType, &size); err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if user.Role == "salesperson" {
		var creatorID string
		if err := a.DB.QueryRow(r.Context(), `SELECT created_by FROM tickets WHERE id=$1`, ticketID).Scan(&creatorID); err != nil || creatorID != user.ID {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}
	reader, storedContentType, storedSize, err := a.Storage.Open(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusNotFound, "stored file not found")
		return
	}
	defer reader.Close()
	if storedContentType != "" {
		contentType = storedContentType
	}
	if storedSize > 0 {
		size = storedSize
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, reader)
}

func (a *App) kpi(w http.ResponseWriter, r *http.Request) {
	var open, inProgress, resolved, rejected int
	var avgSeconds *float64
	err := a.DB.QueryRow(r.Context(), `
		SELECT COUNT(*) FILTER (WHERE status='open'), COUNT(*) FILTER (WHERE status='in_progress'),
		       COUNT(*) FILTER (WHERE status='resolved' AND resolved_at::date=CURRENT_DATE),
		       COUNT(*) FILTER (WHERE status='rejected' AND resolved_at::date=CURRENT_DATE),
		       AVG(EXTRACT(EPOCH FROM (resolved_at-created_at))) FILTER (WHERE resolved_at IS NOT NULL)
		FROM tickets`).Scan(&open, &inProgress, &resolved, &rejected, &avgSeconds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to calculate KPIs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"open": open, "inProgress": inProgress, "resolvedToday": resolved, "rejectedToday": rejected,
		"averageResolutionSeconds": avgSeconds,
	})
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		FullName string `json:"fullName"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !readJSON(w, r, &payload) {
		return
	}
	payload.FullName = strings.TrimSpace(payload.FullName)
	payload.Email = strings.ToLower(strings.TrimSpace(payload.Email))
	payload.Role = strings.TrimSpace(payload.Role)
	if payload.FullName == "" || payload.Email == "" || len(payload.Password) < 8 || (payload.Role != "salesperson" && payload.Role != "support") {
		writeError(w, http.StatusBadRequest, "full name, valid role, email and password (8+ chars) are required")
		return
	}
	hash, err := auth.HashPassword(payload.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var user auth.User
	err = a.DB.QueryRow(r.Context(), `INSERT INTO users(full_name,email,password_hash,role) VALUES($1,$2,$3,$4) RETURNING id,full_name,email,role::text`, payload.FullName, payload.Email, hash, payload.Role).
		Scan(&user.ID, &user.FullName, &user.Email, &user.Role)
	if err != nil {
		writeError(w, http.StatusConflict, "could not create user; email may already exist")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT id, full_name, email, role::text, active, created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, email, role string
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &name, &email, &role, &active, &created); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read users")
			return
		}
		result = append(result, map[string]any{"id": id, "fullName": name, "email": email, "role": role, "active": active, "createdAt": created})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": result})
}

func (a *App) websocket(w http.ResponseWriter, r *http.Request) {
	token := websocketToken(r)
	user, err := auth.ParseToken(a.Cfg.JWTSecret, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid websocket token")
		return
	}
	_ = user
	authProtocol := "ticket-auth." + token
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		Subprotocols:    []string{authProtocol},
		CheckOrigin: func(req *http.Request) bool {
			origin := req.Header.Get("Origin")
			if origin == "" {
				return true
			}
			for _, allowed := range a.Cfg.AllowedOrigins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &realtime.Client{Conn: conn, Send: make(chan []byte, 32)}
	a.Hub.Register(client)
	defer a.Hub.Unregister(client)

	go func() {
		for message := range client.Send {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()
	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func websocketToken(r *http.Request) string {
	protocols := r.Header.Values("Sec-WebSocket-Protocol")
	for _, header := range protocols {
		for _, value := range strings.Split(header, ",") {
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, "ticket-auth.") {
				return strings.TrimPrefix(value, "ticket-auth.")
			}
		}
	}
	return ""
}

func (a *App) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, origin := range a.Cfg.AllowedOrigins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func safeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "..", "_")
	if name == "" {
		return "evidence"
	}
	return name
}

func allowedContentType(contentType, fileName string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	ext := strings.ToLower(filepath.Ext(fileName))
	allowed := map[string]bool{
		"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true,
		"application/pdf": true,
		"audio/mpeg":      true, "audio/wav": true, "audio/ogg": true, "audio/mp4": true, "audio/webm": true,
		"video/mp4": true, "video/webm": true, "video/quicktime": true, "video/x-msvideo": true, "video/mpeg": true,
	}
	if allowed[contentType] {
		return true
	}
	return extAllowed(ext)
}

func extAllowed(ext string) bool {
	for _, e := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".pdf", ".mp3", ".wav", ".ogg", ".m4a", ".mp4", ".webm", ".mov", ".avi", ".mpeg", ".mpg"} {
		if ext == e {
			return true
		}
	}
	return false
}

func validPriority(value string) bool {
	switch value {
	case "low", "normal", "high", "urgent":
		return true
	}
	return false
}
func validStatus(value string) bool {
	switch value {
	case "open", "in_progress", "resolved", "rejected":
		return true
	}
	return false
}
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := decoder.Decode(dest); err != nil {
		slog.Warn("request validation failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
