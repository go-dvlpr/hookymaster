// Package api exposes the use cases over HTTP with Fiber.
package api

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/rs/zerolog"

	"github.com/godvlpr/hookymaster/internal/app"
)

const maxUpload = 10 << 20

// Service is what the handlers need from the application layer.
type Service interface {
	Nav(ctx context.Context) (app.Nav, error)
	Overview(ctx context.Context) (app.Overview, error)
	CreateProject(ctx context.Context, in app.CreateProjectInput) (string, error)
	Project(ctx context.Context, code string) (app.ProjectView, error)
	UpdateSettings(ctx context.Context, code string, st app.Settings) (app.Settings, error)
	DeleteProject(ctx context.Context, code string) error
	AddHook(ctx context.Context, code string, in app.HookInput) (string, error)
	UpdateHook(ctx context.Context, id int64, in app.HookInput) error
	DeleteHook(ctx context.Context, id int64) error
	DeleteHooks(ctx context.Context, code string, ids []int64) error
	ImportMeta(ctx context.Context, code, fileName string, r io.Reader) (app.ImportView, error)
	LatestImport(ctx context.Context, code string) (app.ImportView, error)
	AssignAdNames(ctx context.Context, code string, items []app.Assignment) (app.ImportView, error)
	GenerateOptions(ctx context.Context, code string) (app.GenerateOptions, error)
	Generate(ctx context.Context, code string, in app.GenerateInput) (app.CandidatesView, error)
	Candidates(ctx context.Context, code string) (app.CandidatesView, error)
	SelectCandidate(ctx context.Context, id int64, in app.HookInput) error
	RejectCandidate(ctx context.Context, id int64) error
	UndoCandidate(ctx context.Context, id int64) error
	Queue(ctx context.Context, status string) (app.QueueView, error)
	Export(ctx context.Context, ids []int64) ([]byte, error)
}

type handler struct {
	svc Service
	log zerolog.Logger
}

// New builds the Fiber app. webDir holds the built frontend; it is skipped
// when empty.
func New(svc Service, log zerolog.Logger, webDir string) *fiber.App {
	h := &handler{svc: svc, log: log}
	f := fiber.New(fiber.Config{
		BodyLimit:    maxUpload + 1<<20,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 180 * time.Second, // generation waits for the model
		ErrorHandler: h.errorHandler,
	})
	f.Use(recover.New())

	r := f.Group("/api")
	r.Get("/nav", h.nav)
	r.Get("/overview", h.overview)
	r.Post("/projects", h.createProject)
	r.Get("/projects/:code", h.project)
	r.Put("/projects/:code/settings", h.updateSettings)
	r.Delete("/projects/:code", h.deleteProject)
	r.Post("/projects/:code/hooks", h.addHook)
	r.Get("/projects/:code/import", h.latestImport)
	r.Post("/projects/:code/import", h.importMeta)
	r.Post("/projects/:code/import/assign", h.assignAdNames)
	r.Get("/projects/:code/generate", h.generateOptions)
	r.Post("/projects/:code/generate", h.generate)
	r.Get("/projects/:code/candidates", h.candidates)
	r.Put("/hooks/:id", h.updateHook)
	r.Delete("/hooks/:id", h.deleteHook)
	r.Post("/projects/:code/hooks/delete", h.deleteHooks)
	r.Post("/candidates/:id/select", h.selectCandidate)
	r.Post("/candidates/:id/reject", h.rejectCandidate)
	r.Post("/candidates/:id/undo", h.undoCandidate)
	r.Get("/queue", h.queue)
	r.Post("/queue/export", h.export)
	r.Use(func(c fiber.Ctx) error { return fiber.ErrNotFound })

	if webDir != "" {
		index := filepath.Join(webDir, "index.html")
		// index.html must be revalidated, or browsers keep an old UI after a
		// deploy; files in /assets have a content hash in their names.
		noCache := func(c fiber.Ctx) error {
			if !strings.HasPrefix(c.Path(), "/assets/") {
				c.Set(fiber.HeaderCacheControl, "no-cache")
			}
			return c.Next()
		}
		f.Get("/*", noCache, static.New(webDir, static.Config{
			// Client-side routes: the file server already set 404.
			NotFoundHandler: func(c fiber.Ctx) error { return c.Status(fiber.StatusOK).SendFile(index) },
		}))
	}
	return f
}

type errorBody struct {
	Error string `json:"error"`
}

func (h *handler) errorHandler(c fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	msg := "Something went wrong on our side. Try again in a moment."
	var fe *fiber.Error
	switch {
	case errors.Is(err, app.ErrNotFound):
		status, msg = fiber.StatusNotFound, userMessage(err, app.ErrNotFound)
	case errors.Is(err, app.ErrInvalid):
		status, msg = fiber.StatusBadRequest, userMessage(err, app.ErrInvalid)
	case errors.Is(err, app.ErrConflict):
		status, msg = fiber.StatusConflict, userMessage(err, app.ErrConflict)
	case errors.Is(err, app.ErrNoModel):
		status, msg = fiber.StatusServiceUnavailable, "Generation is off. Add OPENAI_API_KEY to .env and restart the app."
	case errors.As(err, &fe):
		status, msg = fe.Code, fe.Message
		if status == fiber.StatusNotFound && msg == fiber.ErrNotFound.Message {
			msg = "This page does not exist."
		}
	default:
		h.log.Error().Err(err).Str("method", c.Method()).Str("path", c.Path()).Msg("request failed")
	}
	return c.Status(status).JSON(errorBody{Error: msg})
}

// userMessage drops the sentinel prefix and starts the sentence with a
// capital letter.
func userMessage(err, sentinel error) string {
	msg := strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
	r, size := utf8.DecodeRuneInString(msg)
	return string(unicode.ToUpper(r)) + msg[size:]
}

func idParam(c fiber.Ctx) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fiber.NewError(fiber.StatusBadRequest, "This link has a wrong id.")
	}
	return id, nil
}

func bind(c fiber.Ctx, v any) error {
	if err := c.Bind().Body(v); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "The request could not be read.")
	}
	return nil
}

func (h *handler) nav(c fiber.Ctx) error {
	v, err := h.svc.Nav(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) overview(c fiber.Ctx) error {
	v, err := h.svc.Overview(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) createProject(c fiber.Ctx) error {
	var in app.CreateProjectInput
	if err := bind(c, &in); err != nil {
		return err
	}
	code, err := h.svc.CreateProject(c.Context(), in)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"code": code})
}

func (h *handler) project(c fiber.Ctx) error {
	v, err := h.svc.Project(c.Context(), c.Params("code"))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) updateSettings(c fiber.Ctx) error {
	var in app.Settings
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := h.svc.UpdateSettings(c.Context(), c.Params("code"), in)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) addHook(c fiber.Ctx) error {
	var in app.HookInput
	if err := bind(c, &in); err != nil {
		return err
	}
	adName, err := h.svc.AddHook(c.Context(), c.Params("code"), in)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"adName": adName})
}

func (h *handler) updateHook(c fiber.Ctx) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	var in app.HookInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := h.svc.UpdateHook(c.Context(), id, in); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) deleteHook(c fiber.Ctx) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteHook(c.Context(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) deleteHooks(c fiber.Ctx) error {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := h.svc.DeleteHooks(c.Context(), c.Params("code"), in.IDs); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) deleteProject(c fiber.Ctx) error {
	if err := h.svc.DeleteProject(c.Context(), c.Params("code")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) assignAdNames(c fiber.Ctx) error {
	var in struct {
		Items []app.Assignment `json:"items"`
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := h.svc.AssignAdNames(c.Context(), c.Params("code"), in.Items)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) importMeta(c fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Choose a CSV file to upload.")
	}
	if fh.Size > maxUpload {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "The file is larger than 10 MB.")
	}
	f, err := fh.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "The file could not be read.")
	}
	defer f.Close()
	v, err := h.svc.ImportMeta(c.Context(), c.Params("code"), filepath.Base(fh.Filename), f)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) latestImport(c fiber.Ctx) error {
	v, err := h.svc.LatestImport(c.Context(), c.Params("code"))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) generateOptions(c fiber.Ctx) error {
	v, err := h.svc.GenerateOptions(c.Context(), c.Params("code"))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) generate(c fiber.Ctx) error {
	var in app.GenerateInput
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := h.svc.Generate(c.Context(), c.Params("code"), in)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) candidates(c fiber.Ctx) error {
	v, err := h.svc.Candidates(c.Context(), c.Params("code"))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) selectCandidate(c fiber.Ctx) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	var in app.HookInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := h.svc.SelectCandidate(c.Context(), id, in); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) rejectCandidate(c fiber.Ctx) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	if err := h.svc.RejectCandidate(c.Context(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) undoCandidate(c fiber.Ctx) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	if err := h.svc.UndoCandidate(c.Context(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) queue(c fiber.Ctx) error {
	v, err := h.svc.Queue(c.Context(), fiber.Query(c, "status", app.QueueReady))
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (h *handler) export(c fiber.Ctx) error {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	csv, err := h.svc.Export(c.Context(), in.IDs)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="hooks_for_production.csv"`)
	return c.Send(csv)
}
