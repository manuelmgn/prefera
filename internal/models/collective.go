package models

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
)

// CollectiveList represents a shared/collective list.
type CollectiveList struct {
	ID             int
	CreatorID      int
	Name           string
	Description    string
	ShareCode      string
	IsPublic       bool   // true = anyone can view; false = participants only
	VotePermission string // "all" = anyone votes; "link" = share link required; "closed" = nobody
	HideItems      bool   // true = items hidden until the user votes; forces Versus mode
	IsActive       bool
	CreatedAt      string
	CreatorName    string // populated via JOIN
	Participants   int    // participant count
	Ranked         int    // number of users who have ranked
	HasVoted       bool   // true if the current user has already ranked in this list
}

// CollectiveItem represents a canonical item in a collective list.
type CollectiveItem struct {
	ID           int
	CollectiveID int
	Name         string
	Description  string
	Link         string
	Image        string
	Position     int
	AvgPosition  float64 // populated in the aggregate result query
	IsTied       bool    // true if this item shares the same AvgPosition as the previous one (tie)
}

// CollectiveParticipant represents a participant in a collective list.
type CollectiveParticipant struct {
	UserID      int
	DisplayName string
	HasRanked   bool
}

// CollectiveUserRanking represents the full ranking submitted by a single user.
type CollectiveUserRanking struct {
	UserID      int
	DisplayName string
	Items       []CollectiveItem
}

// GenerateShareCode generates a unique 8-character alphanumeric code (uppercase).
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

// CreateCollective creates a new collective list with its items.
// The creator is automatically added as a participant.
func CreateCollective(db *sql.DB, creatorID int, name, description string, isPublic bool, votePermission string, hideItems bool, items []ListItemInput) (int, string, error) {
	code, err := GenerateShareCode(db)
	if err != nil {
		return 0, "", err
	}

	// Validate vote_permission
	if votePermission != "link" {
		votePermission = "all"
	}
	// Private lists always require a share link
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

	// The creator is automatically added as a participant
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

// GetCollectiveByID retrieves a collective list by its ID.
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

// GetCollectiveByShareCode retrieves a collective list by its share code.
func GetCollectiveByShareCode(db *sql.DB, code string) (*CollectiveList, error) {
	var id int
	err := db.QueryRow("SELECT id FROM collective_lists WHERE share_code = ?", code).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetCollectiveByID(db, id)
}

// GetCollectiveItems retrieves the canonical items of a collective list.
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

// IsParticipant checks whether a user is already a participant in a collective list.
func IsParticipant(db *sql.DB, collectiveID, userID int) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM collective_participants WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	).Scan(&count)
	return count > 0, err
}

// HasUserRanked checks whether a user has already submitted a ranking.
func HasUserRanked(db *sql.DB, collectiveID, userID int) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM collective_rankings WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	).Scan(&count)
	return count > 0, err
}

// CanUserVote checks whether a user is allowed to vote in a collective list.
func CanUserVote(db *sql.DB, collectiveID, userID int) bool {
	cl, err := GetCollectiveByID(db, collectiveID)
	if err != nil || !cl.IsActive {
		return false
	}
	// Closed lists: nobody can vote
	if cl.VotePermission == "closed" {
		return false
	}
	// "all": any logged-in user can vote
	if cl.VotePermission == "all" {
		return true
	}
	// "link": must be a participant (joined via share link)
	isP, _ := IsParticipant(db, collectiveID, userID)
	return isP
}

// CanUserView checks whether a user is allowed to view a collective list.
func CanUserView(db *sql.DB, collectiveID, userID int) bool {
	cl, err := GetCollectiveByID(db, collectiveID)
	if err != nil {
		return false
	}
	// Public lists: anyone can view
	if cl.IsPublic {
		return true
	}
	// Private lists: participants only
	isP, _ := IsParticipant(db, collectiveID, userID)
	return isP
}

// JoinCollective adds a user as a participant in a collective list.
func JoinCollective(db *sql.DB, collectiveID, userID int) error {
	_, err := db.Exec(
		"INSERT OR IGNORE INTO collective_participants (collective_id, user_id) VALUES (?, ?)",
		collectiveID, userID,
	)
	return err
}

// GetParticipants retrieves all participants of a collective list.
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

// SaveUserRanking saves (or updates) a user's ranking for a collective list.
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

// DeleteUserRanking deletes a user's ranking from a collective list.
func DeleteUserRanking(db *sql.DB, collectiveID, userID int) error {
	_, err := db.Exec(
		"DELETE FROM collective_rankings WHERE collective_id = ? AND user_id = ?",
		collectiveID, userID,
	)
	return err
}

// GetCollectiveResult returns the aggregated result (average position per item).
// Only considers users who have actually submitted a ranking.
func GetCollectiveResult(db *sql.DB, collectiveID int) ([]CollectiveItem, error) {
	// Count how many users have ranked
	var rankedCount int
	db.QueryRow("SELECT COUNT(DISTINCT user_id) FROM collective_rankings WHERE collective_id = ?", collectiveID).Scan(&rankedCount)

	if rankedCount == 0 {
		// No votes yet: return items in canonical order
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

// UpdateCollectiveItemDetails updates the description, link, and image of a collective item.
func UpdateCollectiveItemDetails(db *sql.DB, itemID int, description, link, image string) error {
	_, err := db.Exec(
		"UPDATE collective_items SET description = ?, link = ?, image = ? WHERE id = ?",
		description, link, image, itemID,
	)
	return err
}

// GetAllUserRankings retrieves the individual rankings of all participants.
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

// GetCollectivesForUser retrieves all collective lists the user participates in.
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

// GetPublicCollectives retrieves public collective lists that the user does NOT participate in.
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

// GetLatestCollectivesVisible retrieves the latest N collective lists visible to the user.
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

// GetCollectivesVotedByUser retrieves all collective lists where the user has submitted a ranking.
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
		cl.HasVoted = true // by definition: this query only returns lists where the user has voted
		lists = append(lists, cl)
	}
	return lists, nil
}

// GetLatestPublicCollectives retrieves the latest N public collective lists,
// setting HasVoted=true for each list where the current user has already ranked.
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

// CreateShadowListForVersus creates a temporary "shadow" list to run the Versus engine.
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

// SyncVersusResultToCollective copies the item positions from a shadow list back
// into collective_rankings. It is a no-op if the list is not a shadow list.
// Note: does NOT delete the shadow list — the handler does that after the redirect.
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

// ConvertListToCollective converts an individual list into a collective one.
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

// UpdateCollective updates a collective list's metadata.
func UpdateCollective(db *sql.DB, collectiveID int, name, description string, isPublic bool, votePermission string, hideItems bool) error {
	// Validate vote_permission
	if votePermission != "link" && votePermission != "closed" {
		votePermission = "all"
	}
	// Private and not closed: force "link"
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

	// Closed lists are deactivated; all others remain active
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

// DeleteCollective deletes a collective list and all associated data (via CASCADE).
func DeleteCollective(db *sql.DB, collectiveID int) error {
	_, err := db.Exec("DELETE FROM collective_lists WHERE id = ?", collectiveID)
	return err
}
