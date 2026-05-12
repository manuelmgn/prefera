package models

import (
	"database/sql"
	"time"
)

// List represents a ranking list created by a user.
type List struct {
	ID          int
	UserID      int
	Name        string
	Description string
	IsPublic    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Auxiliary fields (not in the table, populated via JOIN queries)
	AuthorName string
	Items      []ListItem
}

// ListItem represents an item within a list, including its position.
type ListItem struct {
	ID          int
	ListID      int
	Name        string
	Description string
	Link        string
	Image       string
	Position    int
}

// ListItemInput represents an item as received during creation or editing.
type ListItemInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Link        string `json:"link"`
	Image       string `json:"image"`
}

// CreateList creates a new list and its items.
// Returns the ID of the created list.
func CreateList(db *sql.DB, userID int, name, description string, isPublic bool, items []ListItemInput) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO lists (user_id, name, description, is_public) VALUES (?, ?, ?, ?)",
		userID, name, description, isPublic,
	)
	if err != nil {
		return 0, err
	}

	listID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	for i, item := range items {
		_, err = tx.Exec(
			"INSERT INTO list_items (list_id, name, description, link, image, position) VALUES (?, ?, ?, ?, ?, ?)",
			listID, item.Name, item.Description, item.Link, item.Image, i+1,
		)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return int(listID), nil
}

// GetListByID retrieves a list by its ID, including its items ordered by position.
func GetListByID(db *sql.DB, id int) (*List, error) {
	list := &List{}
	err := db.QueryRow(`
		SELECT l.id, l.user_id, l.name, l.description, l.is_public,
		       l.created_at, l.updated_at, COALESCE(NULLIF(u.display_name,''), u.username)
		FROM lists l
		JOIN users u ON l.user_id = u.id
		WHERE l.id = ?
	`, id).Scan(
		&list.ID, &list.UserID, &list.Name, &list.Description,
		&list.IsPublic, &list.CreatedAt, &list.UpdatedAt, &list.AuthorName,
	)
	if err != nil {
		return nil, err
	}

	// Fetch items ordered by position
	list.Items, err = GetListItems(db, id)
	if err != nil {
		return nil, err
	}

	return list, nil
}

// GetListItems retrieves all items in a list ordered by position.
func GetListItems(db *sql.DB, listID int) ([]ListItem, error) {
	rows, err := db.Query(
		"SELECT id, list_id, name, description, link, image, position FROM list_items WHERE list_id = ? ORDER BY position",
		listID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ListItem
	for rows.Next() {
		var item ListItem
		if err := rows.Scan(&item.ID, &item.ListID, &item.Name, &item.Description, &item.Link, &item.Image, &item.Position); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// GetListsForUser retrieves all lists belonging to a user.
func GetListsForUser(db *sql.DB, userID int) ([]List, error) {
	rows, err := db.Query(`
		SELECT l.id, l.user_id, l.name, l.description, l.is_public,
		       l.created_at, l.updated_at, COALESCE(NULLIF(u.display_name,''), u.username)
		FROM lists l
		JOIN users u ON l.user_id = u.id
		WHERE l.user_id = ? AND l.collective_source_id IS NULL
		ORDER BY l.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Description,
			&l.IsPublic, &l.CreatedAt, &l.UpdatedAt, &l.AuthorName); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}

	// Fetch the top 3 items for each list (used in the dashboard)
	for i := range lists {
		items, err := GetTopItems(db, lists[i].ID, 3)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}

	return lists, nil
}

// GetPublicLists retrieves all public lists belonging to other users.
func GetPublicLists(db *sql.DB, excludeUserID int) ([]List, error) {
	rows, err := db.Query(`
		SELECT l.id, l.user_id, l.name, l.description, l.is_public,
		       l.created_at, l.updated_at, COALESCE(NULLIF(u.display_name,''), u.username)
		FROM lists l
		JOIN users u ON l.user_id = u.id
		WHERE l.is_public = 1 AND l.user_id != ? AND l.collective_source_id IS NULL
		ORDER BY l.updated_at DESC
	`, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Description,
			&l.IsPublic, &l.CreatedAt, &l.UpdatedAt, &l.AuthorName); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}

	// Fetch top 3 items for each public list
	for i := range lists {
		items, err := GetTopItems(db, lists[i].ID, 3)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}

	return lists, nil
}

// GetRecentListsForUser retrieves the user's lists from the last 3 months, up to the given limit.
func GetRecentListsForUser(db *sql.DB, userID, limit int) ([]List, error) {
	rows, err := db.Query(`
		SELECT l.id, l.user_id, l.name, l.description, l.is_public,
		       l.created_at, l.updated_at, COALESCE(NULLIF(u.display_name,''), u.username)
		FROM lists l
		JOIN users u ON l.user_id = u.id
		WHERE l.user_id = ? AND l.collective_source_id IS NULL
		  AND l.created_at >= datetime('now', '-3 months')
		ORDER BY l.created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Description,
			&l.IsPublic, &l.CreatedAt, &l.UpdatedAt, &l.AuthorName); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	for i := range lists {
		items, err := GetTopItems(db, lists[i].ID, 3)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}
	return lists, nil
}

// CountListsForUser returns the total number of individual lists owned by a user.
func CountListsForUser(db *sql.DB, userID int) (int, error) {
	var n int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM lists WHERE user_id = ? AND collective_source_id IS NULL",
		userID,
	).Scan(&n)
	return n, err
}

// GetLatestPublicLists retrieves the most recent public lists from other users, up to the given limit.
func GetLatestPublicLists(db *sql.DB, excludeUserID, limit int) ([]List, error) {
	rows, err := db.Query(`
		SELECT l.id, l.user_id, l.name, l.description, l.is_public,
		       l.created_at, l.updated_at, COALESCE(NULLIF(u.display_name,''), u.username)
		FROM lists l
		JOIN users u ON l.user_id = u.id
		WHERE l.is_public = 1 AND l.user_id != ? AND l.collective_source_id IS NULL
		ORDER BY l.created_at DESC
		LIMIT ?
	`, excludeUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Description,
			&l.IsPublic, &l.CreatedAt, &l.UpdatedAt, &l.AuthorName); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	for i := range lists {
		items, err := GetTopItems(db, lists[i].ID, 3)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}
	return lists, nil
}

// GetTopItems retrieves the top N items from a list ordered by position.
func GetTopItems(db *sql.DB, listID, limit int) ([]ListItem, error) {
	rows, err := db.Query(
		"SELECT id, list_id, name, description, link, image, position FROM list_items WHERE list_id = ? ORDER BY position LIMIT ?",
		listID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ListItem
	for rows.Next() {
		var item ListItem
		if err := rows.Scan(&item.ID, &item.ListID, &item.Name, &item.Description, &item.Link, &item.Image, &item.Position); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// UpdateList updates a list's name, description, and visibility.
func UpdateList(db *sql.DB, listID int, name, description string, isPublic bool) error {
	_, err := db.Exec(`
		UPDATE lists SET name = ?, description = ?, is_public = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, description, isPublic, listID)
	return err
}

// UpdateItemPositions updates the position of each item in a list.
// Receives an ordered slice of item IDs.
func UpdateItemPositions(db *sql.DB, listID int, itemIDs []int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update position for each item
	for i, itemID := range itemIDs {
		_, err = tx.Exec(
			"UPDATE list_items SET position = ? WHERE id = ? AND list_id = ?",
			i+1, itemID, listID,
		)
		if err != nil {
			return err
		}
	}

	// Update the list's updated_at timestamp
	_, err = tx.Exec(
		"UPDATE lists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", listID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteList deletes a list and all its items (via CASCADE).
func DeleteList(db *sql.DB, listID int) error {
	_, err := db.Exec("DELETE FROM lists WHERE id = ?", listID)
	return err
}

// CloneList creates a copy of a public list for another user.
// Items are copied with the same names and order.
func CloneList(db *sql.DB, sourceListID, newUserID int) (int, error) {
	// Fetch the original list
	source, err := GetListByID(db, sourceListID)
	if err != nil {
		return 0, err
	}

	// Extract items with description, link, and image
	items := make([]ListItemInput, len(source.Items))
	for i, item := range source.Items {
		items[i] = ListItemInput{Name: item.Name, Description: item.Description, Link: item.Link, Image: item.Image}
	}

	// Create the new list (private by default)
	return CreateList(db, newUserID, source.Name, source.Description, false, items)
}

// UpdateListItems replaces all items in a list.
func UpdateListItems(db *sql.DB, listID int, items []ListItemInput) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM list_items WHERE list_id = ?", listID)
	if err != nil {
		return err
	}

	for i, item := range items {
		_, err = tx.Exec(
			"INSERT INTO list_items (list_id, name, description, link, image, position) VALUES (?, ?, ?, ?, ?, ?)",
			listID, item.Name, item.Description, item.Link, item.Image, i+1,
		)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(
		"UPDATE lists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", listID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// UpdateItemDetails updates the description, link, and image of an individual item.
func UpdateItemDetails(db *sql.DB, itemID int, description, link, image string) error {
	_, err := db.Exec(
		"UPDATE list_items SET description = ?, link = ?, image = ? WHERE id = ?",
		description, link, image, itemID,
	)
	return err
}
