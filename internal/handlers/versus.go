package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"proj_listas/internal/auth"
	"proj_listas/internal/models"
)

// VersusStart inicia uma nova sessom de Versus para uma lista
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

// VersusPage mostra a pantalla de batalha do Versus
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

	// Verificar se é preciso gerar nova ronda
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

	// Recarregar sessom (pode ter mudado)
	session, _ = models.GetVersusSession(h.db, sessionID)

	// Renderizar página completa (sem layout normal, pantalla completa)
	h.renderFullPage(w, "versus", map[string]interface{}{
		"Session": session,
		"Duel":    duel,
		"Current": session.CompletedComparisons + 1,
		"Total":   session.TotalComparisons,
		"CanUndo": session.CompletedComparisons > 0,
		"ListID":  session.ListID,
	})
}

// VersusNext devolve o próximo duelo (partial para HTMX)
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

// VersusChoose regista a escolha do utilizador
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

// VersusUndo desfaz o último enfrentamento
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

// VersusResult mostra o resultado final com drag & drop.
// Para listas colectivas, sincroniza o resultado e redireciona.
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

	// Aplicar resultados se terminou
	if session.Finished {
		models.ApplyVersusResults(h.db, sessionID)
	}

	// Se é lista sombra colectiva, sincronizar e redirecionar
	var collectiveSourceID sql.NullInt64
	h.db.QueryRow("SELECT collective_source_id FROM lists WHERE id = ?", session.ListID).Scan(&collectiveSourceID)
	if collectiveSourceID.Valid && collectiveSourceID.Int64 > 0 {
		// Sincronizar resultado com a lista colectiva
		models.SyncVersusResultToCollective(h.db, session.ListID)
		// Limpar a lista sombra e a sessom versus (agora é seguro)
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

// VersusSave guarda a ordem final
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
