package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"proj_listas/internal/auth"
	"proj_listas/internal/models"
)

// VersusStart initiates a new Versus session for a list.
func (h *Handler) VersusStart(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	listID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	list, err := models.GetListByID(h.db, listID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}
	if list.UserID != user.ID {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	mode := r.FormValue("mode")
	if mode != "detalhado" {
		mode = "rapido"
	}

	sessionID, err := models.StartVersusSession(h.db, listID, mode)
	if err != nil {
		http.Error(w, "Erro ao iniciar Versus", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/versus/"+strconv.Itoa(sessionID), http.StatusSeeOther)
}

// VersusPage renders the Versus battle screen.
func (h *Handler) VersusPage(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.Atoi(chi.URLParam(r, "sid"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	session, err := models.GetVersusSession(h.db, sessionID)
	if err != nil {
		http.Error(w, "Sessom nom encontrada", http.StatusNotFound)
		return
	}

	if session.Finished {
		http.Redirect(w, r, "/versus/"+strconv.Itoa(sessionID)+"/result", http.StatusSeeOther)
		return
	}

	// Generate next round if needed
	finished, err := models.GenerateNextRoundIfNeeded(h.db, sessionID)
	if err != nil {
		http.Error(w, "Erro no torneio", http.StatusInternalServerError)
		return
	}
	if finished {
		models.ApplyVersusResults(h.db, sessionID)
		http.Redirect(w, r, "/versus/"+strconv.Itoa(sessionID)+"/result", http.StatusSeeOther)
		return
	}

	duel, err := models.GetNextDuel(h.db, sessionID)
	if err != nil || duel == nil {
		models.ApplyVersusResults(h.db, sessionID)
		http.Redirect(w, r, "/versus/"+strconv.Itoa(sessionID)+"/result", http.StatusSeeOther)
		return
	}

	// Reload session (may have changed)
	session, _ = models.GetVersusSession(h.db, sessionID)

	// Render full page without the normal layout (full-screen Versus view)
	h.renderFullPage(w, "versus", map[string]interface{}{
		"Session": session,
		"Duel":    duel,
		"Current": session.CompletedComparisons + 1,
		"Total":   session.TotalComparisons,
		"CanUndo": session.CompletedComparisons > 0,
		"ListID":  session.ListID,
	})
}

// VersusNext returns the next duel as a partial for HTMX.
func (h *Handler) VersusNext(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.Atoi(chi.URLParam(r, "sid"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	finished, err := models.GenerateNextRoundIfNeeded(h.db, sessionID)
	if err != nil {
		http.Error(w, "Erro no torneio", http.StatusInternalServerError)
		return
	}

	session, _ := models.GetVersusSession(h.db, sessionID)

	if finished || session.Finished {
		models.ApplyVersusResults(h.db, sessionID)
		w.Header().Set("HX-Redirect", "/versus/"+strconv.Itoa(sessionID)+"/result")
		return
	}

	duel, err := models.GetNextDuel(h.db, sessionID)
	if err != nil || duel == nil {
		models.ApplyVersusResults(h.db, sessionID)
		w.Header().Set("HX-Redirect", "/versus/"+strconv.Itoa(sessionID)+"/result")
		return
	}

	h.renderPartial(w, "duel", map[string]interface{}{
		"Session": session,
		"Duel":    duel,
		"Current": session.CompletedComparisons + 1,
		"Total":   session.TotalComparisons,
		"CanUndo": session.CompletedComparisons > 0,
	})
}

// VersusChoose records the user's choice for a match.
func (h *Handler) VersusChoose(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.Atoi(chi.URLParam(r, "sid"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	matchID, err := strconv.Atoi(r.FormValue("match_id"))
	if err != nil {
		http.Error(w, "Match ID inválido", http.StatusBadRequest)
		return
	}

	winnerID, err := strconv.Atoi(r.FormValue("winner_id"))
	if err != nil {
		http.Error(w, "Winner ID inválido", http.StatusBadRequest)
		return
	}

	if err := models.RecordResult(h.db, sessionID, matchID, winnerID); err != nil {
		http.Error(w, "Erro ao registar resultado", http.StatusInternalServerError)
		return
	}

	finished, err := models.GenerateNextRoundIfNeeded(h.db, sessionID)
	if err != nil {
		http.Error(w, "Erro no torneio", http.StatusInternalServerError)
		return
	}

	if finished {
		models.ApplyVersusResults(h.db, sessionID)
		w.Header().Set("HX-Redirect", "/versus/"+strconv.Itoa(sessionID)+"/result")
		return
	}

	session, _ := models.GetVersusSession(h.db, sessionID)
	duel, err := models.GetNextDuel(h.db, sessionID)
	if err != nil || duel == nil {
		models.ApplyVersusResults(h.db, sessionID)
		w.Header().Set("HX-Redirect", "/versus/"+strconv.Itoa(sessionID)+"/result")
		return
	}

	h.renderPartial(w, "duel", map[string]interface{}{
		"Session": session,
		"Duel":    duel,
		"Current": session.CompletedComparisons + 1,
		"Total":   session.TotalComparisons,
		"CanUndo": session.CompletedComparisons > 0,
	})
}

// VersusUndo reverts the last match.
func (h *Handler) VersusUndo(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.Atoi(chi.URLParam(r, "sid"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	if err := models.UndoLastMatch(h.db, sessionID); err != nil {
		http.Error(w, "Erro ao desfazer", http.StatusInternalServerError)
		return
	}

	session, _ := models.GetVersusSession(h.db, sessionID)
	duel, err := models.GetNextDuel(h.db, sessionID)
	if err != nil || duel == nil {
		http.Error(w, "Erro ao obter duelo", http.StatusInternalServerError)
		return
	}

	h.renderPartial(w, "duel", map[string]interface{}{
		"Session": session,
		"Duel":    duel,
		"Current": session.CompletedComparisons + 1,
		"Total":   session.TotalComparisons,
		"CanUndo": session.CompletedComparisons > 0,
	})
}

// VersusResult renders the final result with drag-and-drop reordering.
// For collective shadow lists, syncs the result and redirects.
func (h *Handler) VersusResult(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sessionID, err := strconv.Atoi(chi.URLParam(r, "sid"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	session, err := models.GetVersusSession(h.db, sessionID)
	if err != nil {
		http.Error(w, "Sessom nom encontrada", http.StatusNotFound)
		return
	}

	// Apply results if the tournament has finished
	if session.Finished {
		models.ApplyVersusResults(h.db, sessionID)
	}

	// If this is a collective shadow list, sync and redirect
	var collectiveSourceID sql.NullInt64
	h.db.QueryRow("SELECT collective_source_id FROM lists WHERE id = ?", session.ListID).Scan(&collectiveSourceID)
	if collectiveSourceID.Valid && collectiveSourceID.Int64 > 0 {
		// Sync the versus result back to the collective list
		models.SyncVersusResultToCollective(h.db, session.ListID)
		// Delete the shadow list and its versus session (safe to do after sync)
		h.db.Exec("DELETE FROM lists WHERE id = ?", session.ListID)
		http.Redirect(w, r, "/collective/"+strconv.Itoa(int(collectiveSourceID.Int64)), http.StatusSeeOther)
		return
	}

	list, err := models.GetListByID(h.db, session.ListID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	h.renderPage(w, "versus_result", map[string]interface{}{
		"User":    user,
		"Title":   "Resultado Versus",
		"Session": session,
		"List":    list,
	})
}

// VersusSave saves the final item order after a Versus session.
func (h *Handler) VersusSave(w http.ResponseWriter, r *http.Request) {
	sessionID, err := strconv.Atoi(chi.URLParam(r, "sid"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	session, err := models.GetVersusSession(h.db, sessionID)
	if err != nil {
		http.Error(w, "Sessom nom encontrada", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, "/lists/"+strconv.Itoa(session.ListID)+"/edit", http.StatusSeeOther)
}
