package media

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	platformmw "github.com/bits-assignment/dating-platform/backend/internal/platform/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type Handler struct {
	svc *Service
	log *slog.Logger
	// uploadLimit caps how many uploads a user can start per window.
	uploadLimit func(http.Handler) http.Handler
}

func NewHandler(svc *Service, log *slog.Logger, uploadLimit func(http.Handler) http.Handler) *Handler {
	if uploadLimit == nil {
		uploadLimit = func(next http.Handler) http.Handler { return next }
	}
	return &Handler{svc: svc, log: log, uploadLimit: uploadLimit}
}

func (h *Handler) RegisterRoutes(r *mux.Router) {
	r.Handle("/media/presign", h.uploadLimit(http.HandlerFunc(h.Presign))).Methods(http.MethodPost)
	r.HandleFunc("/media/assets/{id}/complete", h.Complete).Methods(http.MethodPost)
	r.HandleFunc("/media/assets/{id}", h.Get).Methods(http.MethodGet)
	r.HandleFunc("/media/assets/{id}", h.Delete).Methods(http.MethodDelete)
}

// ProcessHandler adapts the service to the queue so cmd/worker can register it.
func (h *Handler) ProcessHandler() queue.Handler {
	return ProcessHandler(h.svc)
}

func ProcessHandler(svc *Service) queue.Handler {
	return func(ctx context.Context, e queue.Event) error {
		var job struct {
			AssetID uuid.UUID `json:"asset_id"`
		}
		if err := e.Decode(&job); err != nil {
			return err
		}
		if job.AssetID == uuid.Nil {
			return errors.New("media job is missing asset_id")
		}
		if err := svc.Process(ctx, job.AssetID); err != nil {
			if errors.Is(err, ErrAssetNotFound) {
				// The asset was deleted before the job ran; nothing to retry.
				return nil
			}
			return err
		}
		return nil
	}
}

func (h *Handler) Presign(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return
	}

	var req struct {
		ContentType string `json:"content_type"`
		ByteSize    int64  `json:"byte_size"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid body")
		return
	}

	result, err := h.svc.Presign(r.Context(), userID, req.ContentType, req.ByteSize)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	userID, assetID, ok := h.identify(w, r)
	if !ok {
		return
	}

	asset, err := h.svc.Complete(r.Context(), userID, assetID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, asset)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, assetID, ok := h.identify(w, r)
	if !ok {
		return
	}

	asset, err := h.svc.Get(r.Context(), userID, assetID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, asset)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, assetID, ok := h.identify(w, r)
	if !ok {
		return
	}

	if err := h.svc.Delete(r.Context(), userID, assetID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) identify(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}

	assetID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid asset id")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, assetID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAssetNotFound):
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "media asset not found")
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
	case errors.Is(err, ErrUnsupportedType), errors.Is(err, ErrTooLarge), errors.Is(err, ErrUploadMissing):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, err.Error())
	case errors.Is(err, ErrAlreadySubmitted):
		httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, err.Error())
	case errors.Is(err, ErrStorageUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeInternalError, err.Error())
	default:
		h.log.Error("media request failed",
			"error", err,
			"path", r.URL.Path,
			"request_id", platformmw.RequestIDFrom(r.Context()),
		)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "internal server error")
	}
}
