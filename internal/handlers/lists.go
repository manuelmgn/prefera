package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"prefera/internal/auth"
	"prefera/internal/models"
)

// ListCreate renders the form for creating a new list.
func (h *Handler) ListCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.renderPage(w, "list_create", map[string]interface{}{
		"User":              user,
		"Title":             "Criar lista",
		"DefaultVersusMode": user.DefaultVersusMode,
	})
}

// ListSave processes the new list creation form.
func (h *Handler) ListSave(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	name := SanitizeInput(r.FormValue("name"), MaxListNameLen)
	description := SanitizeInput(r.FormValue("description"), MaxDescriptionLen)
	itemsRaw := r.FormValue("items")

	if name == "" || itemsRaw == "" {
		http.Error(w, "Nome e elementos som obrigatórios", http.StatusBadRequest)
		return
	}

	// Items arrive as a JSON array of objects: [{name, description, link}, ...]
	var rawItems []models.ListItemInput
	if err := json.Unmarshal([]byte(itemsRaw), &rawItems); err != nil {
		http.Error(w, "Formato de elementos inválido", http.StatusBadRequest)
		return
	}

	if len(rawItems) > MaxItemsPerList {
		http.Error(w, "Demasiados elementos (máximo "+strconv.Itoa(MaxItemsPerList)+")", http.StatusBadRequest)
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

	if len(cleanItems) == 0 {
		http.Error(w, "Engade ao menos um elemento", http.StatusBadRequest)
		return
	}

	isPublic := user.DefaultPublic
	listID, err := models.CreateList(h.db, user.ID, name, description, isPublic, cleanItems)
	if err != nil {
		http.Error(w, "Erro ao criar lista", http.StatusInternalServerError)
		return
	}

	orderMode := r.FormValue("order_mode")
	if orderMode == "versus" {
		versusMode := r.FormValue("versus_mode")
		if versusMode != "detalhado" {
			versusMode = "rapido"
		}
		sessionID, err := models.StartVersusSession(h.db, listID, versusMode)
		if err != nil {
			http.Redirect(w, r, "/lists/"+strconv.Itoa(listID)+"/edit", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/versus/"+strconv.Itoa(sessionID), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/lists/"+strconv.Itoa(listID)+"/edit", http.StatusSeeOther)
}

// ListView renders a list (public or owned by the current user).
func (h *Handler) ListView(w http.ResponseWriter, r *http.Request) {
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

	if list.UserID != user.ID && !list.IsPublic {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	h.renderPage(w, "list_view", map[string]interface{}{
		"User":          user,
		"Title":         list.Name,
		"List":          list,
		"IsOwner":       list.UserID == user.ID,
		"CreatedAtDate": list.CreatedAt.Format("2/1/2006"),
	})
}

// ListEdit renders the edit form (owner only).
func (h *Handler) ListEdit(w http.ResponseWriter, r *http.Request) {
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

	// CanEditItems: can edit description/link/image if no versus session has been completed
	var completedSessions int
	h.db.QueryRow(
		"SELECT COUNT(*) FROM versus_sessions WHERE list_id = ? AND finished = 1",
		listID,
	).Scan(&completedSessions)
	canEditItems := completedSessions == 0

	// CanManageItems: can add/remove/rename items if no votes exist AND list is under 5 minutes old
	canManageItems := canEditItems && time.Since(list.CreatedAt) < 5*time.Minute

	h.renderPage(w, "list_edit", map[string]interface{}{
		"User":            user,
		"Title":           "Editar: " + list.Name,
		"List":            list,
		"CanEditItems":    canEditItems,
		"CanManageItems":  canManageItems,
		"AllowedPatterns": AllowedPatternsJS(),
	})
}

// ListUpdate updates a list's metadata.
func (h *Handler) ListUpdate(w http.ResponseWriter, r *http.Request) {
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

	name := SanitizeInput(r.FormValue("name"), MaxListNameLen)
	description := SanitizeInput(r.FormValue("description"), MaxDescriptionLen)
	isPublic := r.FormValue("is_public") != ""

	// If name is empty, keep the existing values (e.g. after versus_result save)
	if name == "" {
		name = list.Name
		description = list.Description
	}

	if err := models.UpdateList(h.db, listID, name, description, isPublic); err != nil {
		http.Error(w, "Erro ao actualizar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/lists/"+strconv.Itoa(listID), http.StatusSeeOther)
}

// ListDelete deletes a list.
func (h *Handler) ListDelete(w http.ResponseWriter, r *http.Request) {
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

	if err := models.DeleteList(h.db, listID); err != nil {
		http.Error(w, "Erro ao apagar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ListReorder updates the item order from a JSON array of IDs.
func (h *Handler) ListReorder(w http.ResponseWriter, r *http.Request) {
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

	var itemIDs []int
	if err := json.NewDecoder(r.Body).Decode(&itemIDs); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	if err := models.UpdateItemPositions(h.db, listID, itemIDs); err != nil {
		http.Error(w, "Erro ao reordenar", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ListClone creates a copy of a public list for the current user.
func (h *Handler) ListClone(w http.ResponseWriter, r *http.Request) {
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

	if !list.IsPublic && list.UserID != user.ID {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	newListID, err := models.CloneList(h.db, listID, user.ID)
	if err != nil {
		http.Error(w, "Erro ao clonar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/lists/"+strconv.Itoa(newListID)+"/edit", http.StatusSeeOther)
}

// ListUpdateItemDetails updates the description, link, and image of an item.
func (h *Handler) ListUpdateItemDetails(w http.ResponseWriter, r *http.Request) {
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

	itemID, err := strconv.Atoi(chi.URLParam(r, "itemId"))
	if err != nil {
		http.Error(w, "ID do elemento inválido", http.StatusBadRequest)
		return
	}

	description := SanitizeItemDescription(r.FormValue("description"))
	link := ValidateItemLink(r.FormValue("link"))
	image := ValidateImageURL(r.FormValue("image"))

	if err := models.UpdateItemDetails(h.db, itemID, description, link, image); err != nil {
		http.Error(w, "Erro ao actualizar", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/lists/"+strconv.Itoa(listID)+"/edit", http.StatusSeeOther)
}
