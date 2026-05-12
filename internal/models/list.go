package models

import (
	"database/sql"
	"time"
)

// List representa uma lista/ranking criada por um utilizador
type List struct {
	ID          int
	UserID      int
	Name        string
	Description string
	IsPublic    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Campos auxiliares (nom estám na tabela, preenchidos em consultas JOIN)
	AuthorName string
	Items      []ListItem
}

// ListItem representa um elemento dentro de uma lista, com a sua posiçom
type ListItem struct {
	ID          int
	ListID      int
	Name        string
	Description string
	Link        string
	Image       string
	Position    int
}

// ListItemInput representa um elemento ao ser criado/editado
type ListItemInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Link        string `json:"link"`
	Image       string `json:"image"`
}

// CreateList cria uma nova lista e os seus elementos.
// Retorna o ID da lista criada.
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

// GetListByID obtém uma lista pelo seu ID, incluindo os seus elementos ordenados
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

	// Obter os elementos da lista, ordenados por posiçom
	list.Items, err = GetListItems(db, id)
	if err != nil {
		return nil, err
	}

	return list, nil
}

// GetListItems obtém os elementos de uma lista ordenados por posiçom
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

// GetListsForUser obtém todas as listas de um utilizador
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

	// Para cada lista, obter os 3 primeiros elementos (para o dashboard)
	for i := range lists {
		items, err := GetTopItems(db, lists[i].ID, 3)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}

	return lists, nil
}

// GetPublicLists obtém todas as listas públicas de outros utilizadores
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

	// Obter o top 3 de cada lista pública
	for i := range lists {
		items, err := GetTopItems(db, lists[i].ID, 3)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}

	return lists, nil
}

// GetRecentListsForUser obtém as listas do utilizador dos últimos 3 meses, limitadas
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

// CountListsForUser conta o total de listas individuais do utilizador
func CountListsForUser(db *sql.DB, userID int) (int, error) {
	var n int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM lists WHERE user_id = ? AND collective_source_id IS NULL",
		userID,
	).Scan(&n)
	return n, err
}

// GetLatestPublicLists obtém as últimas listas públicas de outros utilizadores, limitadas
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

// GetTopItems obtém os N primeiros elementos de uma lista (por posiçom)
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

// UpdateList actualiza o nome, descriçom e visibilidade de uma lista
func UpdateList(db *sql.DB, listID int, name, description string, isPublic bool) error {
	_, err := db.Exec(`
		UPDATE lists SET name = ?, description = ?, is_public = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, description, isPublic, listID)
	return err
}

// UpdateItemPositions actualiza a posiçom de cada elemento numa lista.
// Recebe uma lista de IDs de itens na nova ordem.
func UpdateItemPositions(db *sql.DB, listID int, itemIDs []int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Actualizar a posiçom de cada item
	for i, itemID := range itemIDs {
		_, err = tx.Exec(
			"UPDATE list_items SET position = ? WHERE id = ? AND list_id = ?",
			i+1, itemID, listID,
		)
		if err != nil {
			return err
		}
	}

	// Actualizar o timestamp da lista
	_, err = tx.Exec(
		"UPDATE lists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?", listID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteList apaga uma lista e todos os seus elementos (CASCADE)
func DeleteList(db *sql.DB, listID int) error {
	_, err := db.Exec("DELETE FROM lists WHERE id = ?", listID)
	return err
}

// CloneList cria uma cópia de uma lista pública para outro utilizador.
// Os elementos som copiados com os mesmos nomes e ordem.
func CloneList(db *sql.DB, sourceListID, newUserID int) (int, error) {
	// Obter a lista original
	source, err := GetListByID(db, sourceListID)
	if err != nil {
		return 0, err
	}

	// Extrair os elementos com descriçom, link e imagem
	items := make([]ListItemInput, len(source.Items))
	for i, item := range source.Items {
		items[i] = ListItemInput{Name: item.Name, Description: item.Description, Link: item.Link, Image: item.Image}
	}

	// Criar a nova lista (privada por defeito)
	return CreateList(db, newUserID, source.Name, source.Description, false, items)
}

// UpdateListItems substitui todos os elementos de uma lista.
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

// UpdateItemDetails actualiza a descriçom, link e imagem de um elemento individual
func UpdateItemDetails(db *sql.DB, itemID int, description, link, image string) error {
	_, err := db.Exec(
		"UPDATE list_items SET description = ?, link = ?, image = ? WHERE id = ?",
		description, link, image, itemID,
	)
	return err
}
