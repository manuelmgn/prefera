package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"proj_listas/internal/auth"
	"proj_listas/internal/models"
)

// CollectiveCreate mostra o formulário para criar uma lista colectiva
func (h *Handler) CollectiveCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.renderPage(w, "collective_create", map[string]interface{}{
		"User":              user,
		"Title":             "Criar lista colectiva",
		"DefaultVersusMode": user.DefaultVersusMode,
		"DefaultPublic":     user.DefaultPublic,
	})
}

// CollectiveSave processa a criaçom de uma lista colectiva
func (h *Handler) CollectiveSave(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	name := SanitizeInput(r.FormValue("name"), MaxListNameLen)
	description := SanitizeInput(r.FormValue("description"), MaxDescriptionLen)
	itemsRaw := r.FormValue("items")
	isPublic := r.FormValue("is_public") != ""
	votePermission := r.FormValue("vote_permission")
	hideItems := r.FormValue("hide_items") != ""

	if name == "" || itemsRaw == "" {
		http.Error(w, "Nome e elementos som obrigatórios", http.StatusBadRequest)
		return
	}

	var rawItems []models.ListItemInput
	if err := json.Unmarshal([]byte(itemsRaw), &rawItems); err != nil {
		http.Error(w, "Formato de elementos inválido", http.StatusBadRequest)
		return
	}

	if len(rawItems) > MaxItemsPerList {
		http.Error(w, "Demasiados elementos", http.StatusBadRequest)
		return
	}

	var cleanItems []models.ListItemInput
	for _, item := range rawItems {
		item.Name = SanitizeInput(item.Name, MaxItemNameLen)
		if item.Name == "" {
			continue
		}
		item.Description = SanitizeItemDescription(item.Description)
		item.Link = ValidateItemLink(item.Link)
		item.Image = ValidateImageURL(item.Image)
		cleanItems = append(cleanItems, item)
	}

	if len(cleanItems) < 2 {
		http.Error(w, "Engade ao menos 2 elementos", http.StatusBadRequest)
		return
	}

	collectiveID, _, err := models.CreateCollective(h.db, user.ID, name, description, isPublic, votePermission, hideItems, cleanItems)
	if err != nil {
		http.Error(w, "Erro ao criar lista colectiva", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/collective/"+strconv.Itoa(collectiveID), http.StatusSeeOther)
}

// CollectiveView mostra uma lista colectiva com resultados agregados e individuais
func (h *Handler) CollectiveView(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	// Verificar permissom de visualizaçom
	isParticipant, _ := models.IsParticipant(h.db, collectiveID, user.ID)
	if !cl.IsPublic && !isParticipant {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	// Verificar se pode votar
	canVote := false
	if cl.VotePermission != "closed" && cl.IsActive {
		if cl.VotePermission == "all" && !isParticipant {
			canVote = true // pode votar, será juntado ao votar
		} else if isParticipant {
			canVote = true
		}
	}

	result, err := models.GetCollectiveResult(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Erro ao obter resultados", http.StatusInternalServerError)
		return
	}

	userRankings, err := models.GetAllUserRankings(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Erro ao obter rankings", http.StatusInternalServerError)
		return
	}

	participants, err := models.GetParticipants(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Erro ao obter participantes", http.StatusInternalServerError)
		return
	}

	hasRanked, _ := models.HasUserRanked(h.db, collectiveID, user.ID)
	items, _ := models.GetCollectiveItems(h.db, collectiveID)

	// Se hide_items está activo, ocultar resultado e rankings para quem nom votou
	// O criador vê sempre tudo
	showResult := true
	showItems := true
	if cl.HideItems && !hasRanked && cl.CreatorID != user.ID {
		showResult = false
		showItems = false
	}

	var visibleResult []models.CollectiveItem
	var visibleRankings []models.CollectiveUserRanking
	var visibleItems []models.CollectiveItem
	if showResult {
		visibleResult = result
		visibleRankings = userRankings
	}
	if showItems {
		visibleItems = items
	}

	// Formatar data de criaçom (SQLite devolve "2006-01-02 15:04:05")
	createdAtDate := cl.CreatedAt
	if len(cl.CreatedAt) >= 10 {
		if t, err := time.Parse("2006-01-02", cl.CreatedAt[:10]); err == nil {
			createdAtDate = t.Format("2/1/2006")
		}
	}

	h.renderPage(w, "collective_view", map[string]interface{}{
		"User":          user,
		"Title":         cl.Name,
		"Collective":    cl,
		"Result":        visibleResult,
		"UserRankings":  visibleRankings,
		"Participants":  participants,
		"HasRanked":     hasRanked,
		"CanVote":       canVote,
		"IsCreator":     cl.CreatorID == user.ID,
		"IsParticipant": isParticipant,
		"Items":         visibleItems,
		"HideItems":     cl.HideItems && !hasRanked && cl.CreatorID != user.ID,
		"CreatedAtDate": createdAtDate,
	})
}

// CollectiveJoinDirect permite unir-se via link directo com share_code
func (h *Handler) CollectiveJoinDirect(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	code := strings.ToUpper(chi.URLParam(r, "code"))

	cl, err := models.GetCollectiveByShareCode(h.db, code)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	if !cl.IsActive {
		http.Error(w, "Esta lista colectiva está fechada", http.StatusGone)
		return
	}

	// Juntar automaticamente como participante
	models.JoinCollective(h.db, cl.ID, user.ID)

	// Redirecionar para a vista da lista
	http.Redirect(w, r, "/collective/"+strconv.Itoa(cl.ID), http.StatusSeeOther)
}

// CollectiveReorder guarda a orde manual de um utilizador via drag & drop
func (h *Handler) CollectiveReorder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	// Verificar se pode votar
	if !models.CanUserVote(h.db, collectiveID, user.ID) {
		http.Error(w, "Sem permissom para votar", http.StatusForbidden)
		return
	}

	// Se hide_items está activo e o utilizador nom votou, bloquear orde manual
	if cl.HideItems {
		hasRanked, _ := models.HasUserRanked(h.db, collectiveID, user.ID)
		if !hasRanked {
			http.Error(w, "Elementos ocultos — usa o modo versus", http.StatusForbidden)
			return
		}
	}

	// Se vote_permission é "all", juntar automaticamente como participante
	if cl.VotePermission == "all" {
		models.JoinCollective(h.db, collectiveID, user.ID)
	}

	var itemIDs []int
	if err := json.NewDecoder(r.Body).Decode(&itemIDs); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	positions := make(map[int]int)
	for i, id := range itemIDs {
		positions[id] = i + 1
	}

	if err := models.SaveUserRanking(h.db, collectiveID, user.ID, positions); err != nil {
		http.Error(w, "Erro ao gardar ranking", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// CollectiveVersusStart cria uma lista sombra e inicia versus
func (h *Handler) CollectiveVersusStart(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	if !models.CanUserVote(h.db, collectiveID, user.ID) {
		http.Error(w, "Sem permissom para votar", http.StatusForbidden)
		return
	}

	// Se vote_permission é "all", juntar automaticamente
	if cl.VotePermission == "all" {
		models.JoinCollective(h.db, collectiveID, user.ID)
	}

	shadowListID, err := models.CreateShadowListForVersus(h.db, collectiveID, user.ID)
	if err != nil {
		http.Error(w, "Erro ao preparar versus", http.StatusInternalServerError)
		return
	}

	mode := r.FormValue("mode")
	if mode != "detalhado" {
		mode = "rapido"
	}

	sessionID, err := models.StartVersusSession(h.db, shadowListID, mode)
	if err != nil {
		http.Error(w, "Erro ao iniciar versus", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/versus/"+strconv.Itoa(sessionID), http.StatusSeeOther)
}

// CollectiveDeleteVotes apaga os votos de um utilizador numa lista colectiva
func (h *Handler) CollectiveDeleteVotes(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	if err := models.DeleteUserRanking(h.db, collectiveID, user.ID); err != nil {
		http.Error(w, "Erro ao apagar votos", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/collective/"+strconv.Itoa(collectiveID), http.StatusSeeOther)
}

// CollectiveConvertFromList converte uma lista individual numa colectiva
func (h *Handler) CollectiveConvertFromList(w http.ResponseWriter, r *http.Request) {
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

	isPublic := r.FormValue("is_public") != ""
	votePermission := r.FormValue("vote_permission")
	hideItems := r.FormValue("hide_items") != ""

	collectiveID, _, err := models.ConvertListToCollective(h.db, listID, user.ID, isPublic, votePermission, hideItems)
	if err != nil {
		http.Error(w, "Erro ao converter lista", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/collective/"+strconv.Itoa(collectiveID), http.StatusSeeOther)
}

// CollectiveEditPage mostra a página de ediçom de uma lista colectiva
func (h *Handler) CollectiveEditPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	if cl.CreatorID != user.ID {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	participants, err := models.GetParticipants(h.db, collectiveID)
	if err != nil {
		participants = nil
	}

	items, err := models.GetCollectiveItems(h.db, collectiveID)
	if err != nil {
		items = nil
	}

	// CanEditItems: pode editar descriçom/link/imagem se ninguém rankeou ainda
	canEditItems := cl.Ranked == 0

	// CanManageItems: pode engadir/remover/renomear se ninguém rankeou E lista < 5 min
	var createdAt time.Time
	// cl.CreatedAt é string, tentar parsear
	for _, layout := range []string{"2006-01-02T15:04:05Z", "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, parseErr := time.Parse(layout, cl.CreatedAt); parseErr == nil {
			createdAt = t
			break
		}
	}
	canManageItems := canEditItems && !createdAt.IsZero() && time.Since(createdAt) < 5*time.Minute

	h.renderPage(w, "collective_edit", map[string]interface{}{
		"User":            user,
		"Title":           "Editar: " + cl.Name,
		"Collective":      cl,
		"Participants":    participants,
		"Items":           items,
		"CanEditItems":    canEditItems,
		"CanManageItems":  canManageItems,
		"AllowedPatterns": AllowedPatternsJS(),
	})
}

// CollectiveUpdate actualiza os metadados de uma lista colectiva
func (h *Handler) CollectiveUpdate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	if cl.CreatorID != user.ID {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	name := SanitizeInput(r.FormValue("name"), MaxListNameLen)
	description := SanitizeInput(r.FormValue("description"), MaxDescriptionLen)
	isPublic := r.FormValue("is_public") != ""
	votePermission := r.FormValue("vote_permission")
	hideItems := r.FormValue("hide_items") != ""

	if name == "" {
		name = cl.Name
	}

	if err := models.UpdateCollective(h.db, collectiveID, name, description, isPublic, votePermission, hideItems); err != nil {
		http.Error(w, "Erro ao actualizar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/collective/"+strconv.Itoa(collectiveID), http.StatusSeeOther)
}

// CollectiveDelete apaga uma lista colectiva (só o criador)
func (h *Handler) CollectiveDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	if cl.CreatorID != user.ID {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	if err := models.DeleteCollective(h.db, collectiveID); err != nil {
		http.Error(w, "Erro ao apagar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// CollectiveUpdateItemDetails actualiza a descriçom, link e imagem de um elemento colectivo
func (h *Handler) CollectiveUpdateItemDetails(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	collectiveID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	cl, err := models.GetCollectiveByID(h.db, collectiveID)
	if err != nil {
		http.Error(w, "Lista nom encontrada", http.StatusNotFound)
		return
	}

	if cl.CreatorID != user.ID {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	// Só permitir se ninguém rankeou ainda
	if cl.Ranked > 0 {
		http.Error(w, "Nom é possível editar após votos registados", http.StatusForbidden)
		return
	}

	itemID, err := strconv.Atoi(chi.URLParam(r, "itemId"))
	if err != nil {
		http.Error(w, "ID do elemento inválido", http.StatusBadRequest)
		return
	}

	description := SanitizeItemDescription(r.FormValue("description"))
	link := ValidateItemLink(r.FormValue("link"))
	image := ValidateImageURL(r.FormValue("image"))

	if err := models.UpdateCollectiveItemDetails(h.db, itemID, description, link, image); err != nil {
		http.Error(w, "Erro ao actualizar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/collective/"+strconv.Itoa(collectiveID)+"/edit", http.StatusSeeOther)
}
