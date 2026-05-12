package models

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
)

// CollectiveList representa uma lista colectiva/partilhada
type CollectiveList struct {
	ID             int
	CreatorID      int
	Name           string
	Description    string
	ShareCode      string
	IsPublic       bool   // true = todos podem ver; false = só participantes
	VotePermission string // "all" = todos votam; "link" = só com link; "closed" = ninguém
	HideItems      bool   // true = elementos ocultos até votar; força modo versus
	IsActive       bool
	CreatedAt      string
	CreatorName    string // preenchido via JOIN
	Participants   int    // contagem de participantes
	Ranked         int    // quantos já rankearam
	HasVoted       bool   // true se o utilizador actual já rankeou nesta lista
}

// CollectiveItem representa um elemento canónico de uma lista colectiva
type CollectiveItem struct {
	ID           int
	CollectiveID int
	Name         string
	Description  string
	Link         string
	Image        string
	Position     int
	AvgPosition  float64 // preenchido na consulta de resultado
	IsTied       bool    // true se tem o mesmo AvgPosition que o item anterior (empate)
}

// CollectiveParticipant representa um participante
type CollectiveParticipant struct {
	UserID      int
	DisplayName string
	HasRanked   bool
}

// CollectiveUserRanking representa o ranking completo de um utilizador
type CollectiveUserRanking struct {
	UserID      int
	DisplayName string
	Items       []CollectiveItem
}

// GenerateShareCode gera um código único de 8 caracteres alfanuméricos maiúsculos
func GenerateShareCode(db *sql.DB) (string, error) {
	for i := 0; i < 10; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		code := strings.ToUpper(hex.EncodeToString(b))[:8]
		var exists int
		err := db.QueryRow("SELECT COUNT(*) FROM collective_lists WHERE share_code = ?", code).Scan(&exists)
		if err != nil {
			return "", err
		}
		if exists == 0 {
			return code, nil
		}
	}
	return "", sql.ErrNoRows
}

// CreateCollective cria uma nova lista colectiva com os seus elementos.
// O criador é automaticamente adicionado como participante.
func CreateCollective(db *sql.DB, creatorID int, name, description string, isPublic bool, votePermission string, hideItems bool, items []ListItemInput) (int, string, error) {
	code, err := GenerateShareCode(db)
	if err != nil {
		return 0, "", err
	}

	// Validar vote_permission
	if votePermission != "link" {
		votePermission = "all"
	}
	// Se é privada, forçar "link"
	if !isPublic {
		votePermission = "link"
	}

	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}
	hideItemsInt := 0
	if hideItems {
		hideItemsInt = 1
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO collective_lists (creator_id, name, description, share_code, is_public, vote_permission, hide_items) VALUES (?, ?, ?, ?, ?, ?, ?)",
		creatorID, name, description, code, isPublicInt, votePermission, hideItemsInt,
	)
	if err != nil {
		return 0, "", err
	}

	collectiveID64, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	collectiveID := int(collectiveID64)

	for i, item := range items {
		_, err = tx.Exec(
			"INSERT INTO collective_items (collective_id, name, description, link, image, position) VALUES (?, ?, ?, ?, ?, ?)",
			collectiveID, item.Name, item.Description, item.Link, item.Image, i+1,
		)
		if err != nil {
			return 0, "", err
		}
	}

	// O criador é automaticamente participante
	_, err = tx.Exec(
		"INSERT INTO collective_participants (collective_id, user_id) VALUES (?, ?)",
		collectiveID, creatorID,
	)
	if err != nil {
		return 0, "", err
	}

	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	return collectiveID, code, nil
}

// GetCollectiveByID obtém uma lista colectiva pelo ID
func GetCollectiveByID(db *sql.DB, id int) (*CollectiveList, error) {
	cl := &CollectiveList{}
	var isPublicInt, hideItemsInt int
	err := db.QueryRow(`
		SELECT c.id, c.creator_id, c.name, c.description, c.share_code,
		       c.is_public, c.vote_permission, c.hide_items, c.is_active, c.created_at,
		       COALESCE(NULLIF(u.display_name,''), u.username)
		FROM collective_lists c
		JOIN users u ON c.creator_id = u.id
		WHERE c.id = ?
	`, id).Scan(
		&cl.ID, &cl.CreatorID, &cl.Name, &cl.Description, &cl.ShareCode,
		&isPublicInt, &cl.VotePermission, &hideItemsInt, &cl.IsActive, &cl.CreatedAt, &cl.CreatorName,
	)
	if err != nil {
		return nil, err
	}
	cl.IsPublic = isPublicInt == 1
	cl.HideItems = hideItemsInt == 1

	db.QueryRow("SELECT COUNT(*) FROM collective_participants WHERE collective_id = ?", id).Scan(&cl.Participants)
	db.QueryRow("SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = ?", id).Scan(&cl.Ranked)

	return cl, nil
}

// GetCollectiveByShareCode obtém uma lista colectiva pelo código de partilha
func GetCollectiveByShareCode(db *sql.DB, code string) (*CollectiveList, error) {
	var id int
	err := db.QueryRow("SELECT id FROM collective_lists WHERE share_code = ?", code).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetCollectiveByID(db, id)
}

// GetCollectiveItems obtém os elementos canónicos de uma lista colectiva
func GetCollectiveItems(db *sql.DB, collectiveID int) ([]CollectiveItem, error) {
	rows, err := db.Query(
		"SELECT id, collective_id, name, description, link, image, position FROM collective_items WHERE collective_id = ? ORDER BY position",
		collectiveID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CollectiveItem
	for rows.Next() {
		var item CollectiveItem
		if err := rows.Scan(&item.ID, &item.CollectiveID, &item.Name, &item.Description, &item.Link, &item.Image, &item.Position); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// IsParticipant verifica se um utilizador já faz parte de uma lista colectiva
func IsParticipant(db *sql.DB, collectiveID, userID int) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM collective_participants WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	).Scan(&count)
	return count > 0, err
}

// HasUserRanked verifica se um utilizador já rankeou os elementos
func HasUserRanked(db *sql.DB, collectiveID, userID int) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM collective_rankings WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	).Scan(&count)
	return count > 0, err
}

// CanUserVote verifica se um utilizador pode votar numa lista colectiva
func CanUserVote(db *sql.DB, collectiveID, userID int) bool {
	cl, err := GetCollectiveByID(db, collectiveID)
	if err != nil || !cl.IsActive {
		return false
	}
	// Se está fechada, ninguém pode votar
	if cl.VotePermission == "closed" {
		return false
	}
	// Se vote_permission é "all", qualquer utilizador logado pode votar
	if cl.VotePermission == "all" {
		return true
	}
	// Se é "link", precisa ser participante (via link de partilha)
	isP, _ := IsParticipant(db, collectiveID, userID)
	return isP
}

// CanUserView verifica se um utilizador pode ver uma lista colectiva
func CanUserView(db *sql.DB, collectiveID, userID int) bool {
	cl, err := GetCollectiveByID(db, collectiveID)
	if err != nil {
		return false
	}
	// Listas públicas: todos podem ver
	if cl.IsPublic {
		return true
	}
	// Listas privadas: só participantes
	isP, _ := IsParticipant(db, collectiveID, userID)
	return isP
}

// JoinCollective adiciona um utilizador como participante
func JoinCollective(db *sql.DB, collectiveID, userID int) error {
	_, err := db.Exec(
		"INSERT OR IGNORE INTO collective_participants (collective_id, user_id) VALUES (?, ?)",
		collectiveID, userID,
	)
	return err
}

// GetParticipants obtém os participantes de uma lista colectiva
func GetParticipants(db *sql.DB, collectiveID int) ([]CollectiveParticipant, error) {
	rows, err := db.Query(`
		SELECT cp.user_id, COALESCE(NULLIF(u.display_name,''), u.username),
		       CASE WHEN EXISTS(SELECT 1 FROM collective_rankings cr WHERE cr.collective_id = cp.collective_id AND cr.user_id = cp.user_id) THEN 1 ELSE 0 END
		FROM collective_participants cp
		JOIN users u ON cp.user_id = u.id
		WHERE cp.collective_id = ?
		ORDER BY cp.joined_at
	`, collectiveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []CollectiveParticipant
	for rows.Next() {
		var p CollectiveParticipant
		var ranked int
		if err := rows.Scan(&p.UserID, &p.DisplayName, &ranked); err != nil {
			return nil, err
		}
		p.HasRanked = ranked == 1
		participants = append(participants, p)
	}
	return participants, nil
}

// SaveUserRanking guarda (ou actualiza) o ranking de um utilizador.
func SaveUserRanking(db *sql.DB, collectiveID, userID int, itemPositions map[int]int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"DELETE FROM collective_rankings WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	)
	if err != nil {
		return err
	}

	for itemID, position := range itemPositions {
		_, err = tx.Exec(
			"INSERT INTO collective_rankings (collective_id, user_id, item_id, position) VALUES (?, ?, ?, ?)",
			collectiveID, userID, itemID, position,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteUserRanking apaga o ranking de um utilizador numa lista colectiva
func DeleteUserRanking(db *sql.DB, collectiveID, userID int) error {
	_, err := db.Exec(
		"DELETE FROM collective_rankings WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	)
	return err
}

// GetCollectiveResult obtém o resultado agregado (média das posições).
// Só considera utilizadores que efectivamente rankearam.
func GetCollectiveResult(db *sql.DB, collectiveID int) ([]CollectiveItem, error) {
	// Contar quantos utilizadores rankearam
	var rankedCount int
	db.QueryRow("SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = ?", collectiveID).Scan(&rankedCount)

	if rankedCount == 0 {
		// Sem votos: devolver os items na orde canónica
		return GetCollectiveItems(db, collectiveID)
	}

	rows, err := db.Query(`
		SELECT ci.id, ci.name, ci.description, ci.link, ci.image,
		       CAST(SUM(cr.position) AS REAL) / COUNT(cr.position) as avg_pos
		FROM collective_items ci
		INNER JOIN collective_rankings cr ON ci.id = cr.item_id AND cr.collective_id = ci.collective_id
		WHERE ci.collective_id = ?
		GROUP BY ci.id, ci.name, ci.description, ci.link, ci.image
		ORDER BY avg_pos ASC, ci.name ASC
	`, collectiveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CollectiveItem
	pos := 1
	tieGroupPos := 1
	var prevAvg float64
	first := true
	for rows.Next() {
		var item CollectiveItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Link, &item.Image, &item.AvgPosition); err != nil {
			return nil, err
		}
		item.CollectiveID = collectiveID
		if !first && item.AvgPosition == prevAvg {
			item.IsTied = true
			item.Position = tieGroupPos
		} else {
			tieGroupPos = pos
			item.Position = pos
		}
		prevAvg = item.AvgPosition
		first = false
		pos++
		items = append(items, item)
	}
	return items, nil
}

// UpdateCollectiveItemDetails actualiza a descriçom, link e imagem de um elemento colectivo
func UpdateCollectiveItemDetails(db *sql.DB, itemID int, description, link, image string) error {
	_, err := db.Exec(
		"UPDATE collective_items SET description = ?, link = ?, image = ? WHERE id = ?",
		description, link, image, itemID,
	)
	return err
}

// GetAllUserRankings obtém os rankings individuais de todos os participantes
func GetAllUserRankings(db *sql.DB, collectiveID int) ([]CollectiveUserRanking, error) {
	rows, err := db.Query(`
		SELECT cr.user_id, COALESCE(NULLIF(u.display_name,''), u.username),
		       ci.id, ci.name, cr.position
		FROM collective_rankings cr
		JOIN users u ON cr.user_id = u.id
		JOIN collective_items ci ON cr.item_id = ci.id
		WHERE cr.collective_id = ?
		ORDER BY cr.user_id, cr.position
	`, collectiveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rankingMap := make(map[int]*CollectiveUserRanking)
	var order []int

	for rows.Next() {
		var userID int
		var displayName string
		var item CollectiveItem
		if err := rows.Scan(&userID, &displayName, &item.ID, &item.Name, &item.Position); err != nil {
			return nil, err
		}
		if _, ok := rankingMap[userID]; !ok {
			rankingMap[userID] = &CollectiveUserRanking{
				UserID:      userID,
				DisplayName: displayName,
			}
			order = append(order, userID)
		}
		rankingMap[userID].Items = append(rankingMap[userID].Items, item)
	}

	var result []CollectiveUserRanking
	for _, uid := range order {
		result = append(result, *rankingMap[uid])
	}
	return result, nil
}

// GetCollectivesForUser obtém todas as listas colectivas em que o utilizador participa
func GetCollectivesForUser(db *sql.DB, userID int) ([]CollectiveList, error) {
	rows, err := db.Query(`
		SELECT c.id, c.creator_id, c.name, c.description, c.share_code,
		       c.is_public, c.vote_permission, c.hide_items, c.is_active, c.created_at,
		       COALESCE(NULLIF(u.display_name,''), u.username),
		       (SELECT COUNT(*) FROM collective_participants WHERE collective_id = c.id),
		       (SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = c.id)
		FROM collective_lists c
		JOIN users u ON c.creator_id = u.id
		JOIN collective_participants cp ON c.id = cp.collective_id
		WHERE cp.user_id = ?
		ORDER BY c.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CollectiveList
	for rows.Next() {
		var cl CollectiveList
		var isPublicInt, hideItemsInt int
		if err := rows.Scan(&cl.ID, &cl.CreatorID, &cl.Name, &cl.Description,
			&cl.ShareCode, &isPublicInt, &cl.VotePermission, &hideItemsInt, &cl.IsActive, &cl.CreatedAt,
			&cl.CreatorName, &cl.Participants, &cl.Ranked); err != nil {
			return nil, err
		}
		cl.IsPublic = isPublicInt == 1
		cl.HideItems = hideItemsInt == 1
		lists = append(lists, cl)
	}
	return lists, nil
}

// GetPublicCollectives obtém as listas colectivas públicas em que o utilizador NOM participa
func GetPublicCollectives(db *sql.DB, excludeUserID int) ([]CollectiveList, error) {
	rows, err := db.Query(`
		SELECT c.id, c.creator_id, c.name, c.description, c.share_code,
		       c.is_public, c.vote_permission, c.hide_items, c.is_active, c.created_at,
		       COALESCE(NULLIF(u.display_name,''), u.username),
		       (SELECT COUNT(*) FROM collective_participants WHERE collective_id = c.id),
		       (SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = c.id)
		FROM collective_lists c
		JOIN users u ON c.creator_id = u.id
		WHERE c.is_public = 1
		  AND NOT EXISTS (SELECT 1 FROM collective_participants cp WHERE cp.collective_id = c.id AND cp.user_id = ?)
		ORDER BY c.created_at DESC
	`, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CollectiveList
	for rows.Next() {
		var cl CollectiveList
		var isPublicInt, hideItemsInt int
		if err := rows.Scan(&cl.ID, &cl.CreatorID, &cl.Name, &cl.Description,
			&cl.ShareCode, &isPublicInt, &cl.VotePermission, &hideItemsInt, &cl.IsActive, &cl.CreatedAt,
			&cl.CreatorName, &cl.Participants, &cl.Ranked); err != nil {
			return nil, err
		}
		cl.IsPublic = isPublicInt == 1
		cl.HideItems = hideItemsInt == 1
		lists = append(lists, cl)
	}
	return lists, nil
}

// GetLatestCollectivesVisible obtém as últimas N listas colectivas visíveis pelo utilizador
func GetLatestCollectivesVisible(db *sql.DB, userID, limit int) ([]CollectiveList, error) {
	rows, err := db.Query(`
		SELECT c.id, c.creator_id, c.name, c.description, c.share_code,
		       c.is_public, c.vote_permission, c.hide_items, c.is_active, c.created_at,
		       COALESCE(NULLIF(u.display_name,''), u.username),
		       (SELECT COUNT(*) FROM collective_participants WHERE collective_id = c.id),
		       (SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = c.id)
		FROM collective_lists c
		JOIN users u ON c.creator_id = u.id
		JOIN collective_participants cp ON c.id = cp.collective_id
		WHERE cp.user_id = ?
		ORDER BY c.created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CollectiveList
	for rows.Next() {
		var cl CollectiveList
		var isPublicInt, hideItemsInt int
		if err := rows.Scan(&cl.ID, &cl.CreatorID, &cl.Name, &cl.Description,
			&cl.ShareCode, &isPublicInt, &cl.VotePermission, &hideItemsInt, &cl.IsActive, &cl.CreatedAt,
			&cl.CreatorName, &cl.Participants, &cl.Ranked); err != nil {
			return nil, err
		}
		cl.IsPublic = isPublicInt == 1
		cl.HideItems = hideItemsInt == 1
		lists = append(lists, cl)
	}
	return lists, nil
}

// GetCollectivesVotedByUser obtém todas as listas colectivas em que o utilizador votou
func GetCollectivesVotedByUser(db *sql.DB, userID int) ([]CollectiveList, error) {
	rows, err := db.Query(`
		SELECT DISTINCT c.id, c.creator_id, c.name, c.description, c.share_code,
		       c.is_public, c.vote_permission, c.hide_items, c.is_active, c.created_at,
		       COALESCE(NULLIF(u.display_name,''), u.username),
		       (SELECT COUNT(*) FROM collective_participants WHERE collective_id = c.id),
		       (SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = c.id)
		FROM collective_lists c
		JOIN users u ON c.creator_id = u.id
		INNER JOIN collective_rankings cr ON c.id = cr.collective_id AND cr.user_id = ?
		ORDER BY c.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CollectiveList
	for rows.Next() {
		var cl CollectiveList
		var isPublicInt, hideItemsInt int
		if err := rows.Scan(&cl.ID, &cl.CreatorID, &cl.Name, &cl.Description,
			&cl.ShareCode, &isPublicInt, &cl.VotePermission, &hideItemsInt, &cl.IsActive, &cl.CreatedAt,
			&cl.CreatorName, &cl.Participants, &cl.Ranked); err != nil {
			return nil, err
		}
		cl.IsPublic = isPublicInt == 1
		cl.HideItems = hideItemsInt == 1
		cl.HasVoted = true // por definição: esta consulta só retorna listas onde o utilizador votou
		lists = append(lists, cl)
	}
	return lists, nil
}

// GetLatestPublicCollectives obtém as últimas N listas colectivas públicas,
// marcando HasVoted=true se o utilizador actual já rankeou em cada uma.
func GetLatestPublicCollectives(db *sql.DB, userID, limit int) ([]CollectiveList, error) {
	rows, err := db.Query(`
		SELECT c.id, c.creator_id, c.name, c.description, c.share_code,
		       c.is_public, c.vote_permission, c.hide_items, c.is_active, c.created_at,
		       COALESCE(NULLIF(u.display_name,''), u.username),
		       (SELECT COUNT(*) FROM collective_participants WHERE collective_id = c.id),
		       (SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = c.id),
		       CASE WHEN EXISTS(
		           SELECT 1 FROM collective_rankings WHERE collective_id = c.id AND user_id = ?
		       ) THEN 1 ELSE 0 END
		FROM collective_lists c
		JOIN users u ON c.creator_id = u.id
		WHERE c.is_public = 1
		ORDER BY c.created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CollectiveList
	for rows.Next() {
		var cl CollectiveList
		var isPublicInt, hideItemsInt, hasVotedInt int
		if err := rows.Scan(&cl.ID, &cl.CreatorID, &cl.Name, &cl.Description,
			&cl.ShareCode, &isPublicInt, &cl.VotePermission, &hideItemsInt, &cl.IsActive, &cl.CreatedAt,
			&cl.CreatorName, &cl.Participants, &cl.Ranked, &hasVotedInt); err != nil {
			return nil, err
		}
		cl.IsPublic = isPublicInt == 1
		cl.HideItems = hideItemsInt == 1
		cl.HasVoted = hasVotedInt == 1
		lists = append(lists, cl)
	}
	return lists, nil
}

// CreateShadowListForVersus cria uma lista "sombra" temporária para usar o versus existente
func CreateShadowListForVersus(db *sql.DB, collectiveID, userID int) (int, error) {
	cl, err := GetCollectiveByID(db, collectiveID)
	if err != nil {
		return 0, err
	}

	items, err := GetCollectiveItems(db, collectiveID)
	if err != nil {
		return 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO lists (user_id, name, description, is_public, collective_source_id) VALUES (?, ?, ?, 0, ?)",
		userID, cl.Name, cl.Description, collectiveID,
	)
	if err != nil {
		return 0, err
	}

	listID64, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	listID := int(listID64)

	for _, item := range items {
		_, err = tx.Exec(
			"INSERT INTO list_items (list_id, name, description, link, image, position) VALUES (?, ?, ?, ?, ?, ?)",
			listID, item.Name, item.Description, item.Link, item.Image, item.Position,
		)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return listID, nil
}

// SyncVersusResultToCollective copia as posições de uma lista sombra de volta
// para collective_rankings. É um no-op se a lista nom for sombra.
// Nota: NOM apaga a lista sombra (isso é feito pelo handler depois do redirect).
func SyncVersusResultToCollective(db *sql.DB, listID int) error {
	var collectiveID sql.NullInt64
	var userID int
	err := db.QueryRow(
		"SELECT collective_source_id, user_id FROM lists WHERE id = ?", listID,
	).Scan(&collectiveID, &userID)
	if err != nil || !collectiveID.Valid {
		return nil
	}

	cID := int(collectiveID.Int64)

	collectiveItems, err := GetCollectiveItems(db, cID)
	if err != nil {
		return err
	}

	nameToID := make(map[string]int)
	for _, ci := range collectiveItems {
		nameToID[ci.Name] = ci.ID
	}

	shadowItems, err := GetListItems(db, listID)
	if err != nil {
		return err
	}

	itemPositions := make(map[int]int)
	for _, si := range shadowItems {
		if ciID, ok := nameToID[si.Name]; ok {
			itemPositions[ciID] = si.Position
		}
	}

	if len(itemPositions) > 0 {
		return SaveUserRanking(db, cID, userID, itemPositions)
	}
	return nil
}

// ConvertListToCollective converte uma lista individual numa colectiva.
func ConvertListToCollective(db *sql.DB, listID, userID int, isPublic bool, votePermission string, hideItems bool) (int, string, error) {
	list, err := GetListByID(db, listID)
	if err != nil {
		return 0, "", err
	}

	if list.UserID != userID {
		return 0, "", sql.ErrNoRows
	}

	code, err := GenerateShareCode(db)
	if err != nil {
		return 0, "", err
	}

	if votePermission != "link" {
		votePermission = "all"
	}
	if !isPublic {
		votePermission = "link"
	}
	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}
	hideItemsInt := 0
	if hideItems {
		hideItemsInt = 1
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO collective_lists (creator_id, name, description, share_code, is_public, vote_permission, hide_items) VALUES (?, ?, ?, ?, ?, ?, ?)",
		userID, list.Name, list.Description, code, isPublicInt, votePermission, hideItemsInt,
	)
	if err != nil {
		return 0, "", err
	}

	collectiveID64, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	collectiveID := int(collectiveID64)

	itemPositions := make(map[int]int)
	for _, item := range list.Items {
		res, err := tx.Exec(
			"INSERT INTO collective_items (collective_id, name, description, link, image, position) VALUES (?, ?, ?, ?, ?, ?)",
			collectiveID, item.Name, item.Description, item.Link, item.Image, item.Position,
		)
		if err != nil {
			return 0, "", err
		}
		ciID64, _ := res.LastInsertId()
		itemPositions[int(ciID64)] = item.Position
	}

	_, err = tx.Exec(
		"INSERT INTO collective_participants (collective_id, user_id) VALUES (?, ?)",
		collectiveID, userID,
	)
	if err != nil {
		return 0, "", err
	}

	for ciID, pos := range itemPositions {
		_, err = tx.Exec(
			"INSERT INTO collective_rankings (collective_id, user_id, item_id, position) VALUES (?, ?, ?, ?)",
			collectiveID, userID, ciID, pos,
		)
		if err != nil {
			return 0, "", err
		}
	}

	tx.Exec("DELETE FROM lists WHERE id = ?", listID)

	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	return collectiveID, code, nil
}

// UpdateCollective actualiza os metadados de uma lista colectiva
func UpdateCollective(db *sql.DB, collectiveID int, name, description string, isPublic bool, votePermission string, hideItems bool) error {
	// Validar vote_permission
	if votePermission != "link" && votePermission != "closed" {
		votePermission = "all"
	}
	// Se é privada e nom está fechada, forçar "link"
	if !isPublic && votePermission == "all" {
		votePermission = "link"
	}

	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}
	hideItemsInt := 0
	if hideItems {
		hideItemsInt = 1
	}

	// Se está fechada, desactivar; senón, activar
	isActive := 1
	if votePermission == "closed" {
		isActive = 0
	}

	_, err := db.Exec(
		"UPDATE collective_lists SET name = ?, description = ?, is_public = ?, vote_permission = ?, hide_items = ?, is_active = ? WHERE id = ?",
		name, description, isPublicInt, votePermission, hideItemsInt, isActive, collectiveID,
	)
	return err
}

// DeleteCollective apaga uma lista colectiva e todos os dados associados (CASCADE)
func DeleteCollective(db *sql.DB, collectiveID int) error {
	_, err := db.Exec("DELETE FROM collective_lists WHERE id = ?", collectiveID)
	return err
}
