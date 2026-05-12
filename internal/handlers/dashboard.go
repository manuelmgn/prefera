package handlers

import (
	"net/http"

	"prefera/internal/auth"
	"prefera/internal/models"
)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	recentLists, err := models.GetRecentListsForUser(h.db, user.ID, 4)
	if err != nil {
		http.Error(w, "Erro ao obter listas", http.StatusInternalServerError)
		return
	}

	totalLists, err := models.CountListsForUser(h.db, user.ID)
	if err != nil {
		http.Error(w, "Erro ao contar listas", http.StatusInternalServerError)
		return
	}

	collectiveLists, err := models.GetLatestPublicCollectives(h.db, user.ID, 10)
	if err != nil {
		http.Error(w, "Erro ao obter listas colectivas", http.StatusInternalServerError)
		return
	}

	publicLists, err := models.GetLatestPublicLists(h.db, user.ID, 6)
	if err != nil {
		http.Error(w, "Erro ao obter listas públicas", http.StatusInternalServerError)
		return
	}

	h.renderPage(w, "dashboard", map[string]interface{}{
		"User":            user,
		"Title":           "Início",
		"RecentLists":     recentLists,
		"TotalLists":      totalLists,
		"HasNoRecent":     len(recentLists) == 0 && totalLists > 0,
		"CollectiveLists": collectiveLists,
		"PublicLists":     publicLists,
	})
}

func (h *Handler) MyListsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	allLists, err := models.GetListsForUser(h.db, user.ID)
	if err != nil {
		http.Error(w, "Erro ao obter listas", http.StatusInternalServerError)
		return
	}

	votedCollectives, err := models.GetCollectivesVotedByUser(h.db, user.ID)
	if err != nil {
		http.Error(w, "Erro ao obter colectivas", http.StatusInternalServerError)
		return
	}

	const pageLimit = 10
	showAllLists := len(allLists) > pageLimit
	showAllCollectives := len(votedCollectives) > pageLimit
	if showAllLists {
		allLists = allLists[:pageLimit]
	}
	if showAllCollectives {
		votedCollectives = votedCollectives[:pageLimit]
	}

	h.renderPage(w, "my_lists", map[string]interface{}{
		"User":               user,
		"Title":              "As minhas listas",
		"Lists":              allLists,
		"VotedCollectives":   votedCollectives,
		"ShowAllLists":       showAllLists,
		"ShowAllCollectives": showAllCollectives,
	})
}

func (h *Handler) MyListsAllPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	allLists, err := models.GetListsForUser(h.db, user.ID)
	if err != nil {
		http.Error(w, "Erro ao obter listas", http.StatusInternalServerError)
		return
	}

	h.renderPage(w, "my_lists_all", map[string]interface{}{
		"User":  user,
		"Title": "Todas as minhas listas",
		"Lists": allLists,
	})
}

func (h *Handler) MyListsCollectivesPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	votedCollectives, err := models.GetCollectivesVotedByUser(h.db, user.ID)
	if err != nil {
		http.Error(w, "Erro ao obter colectivas", http.StatusInternalServerError)
		return
	}

	h.renderPage(w, "my_lists_collectives", map[string]interface{}{
		"User":             user,
		"Title":            "Colectivas em que votei",
		"VotedCollectives": votedCollectives,
	})
}
