package models

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// VersusSession representa uma sessom de torneio suíço em curso
type VersusSession struct {
	ID                   int
	ListID               int
	Mode                 string // "rapido" ou "detalhado"
	TotalComparisons     int
	CompletedComparisons int
	IsRoundRobin         bool
	CurrentRound         int
	Finished             bool
}

// VersusMatch representa um enfrentamento individual entre dois elementos
type VersusMatch struct {
	ID         int
	SessionID  int
	Round      int
	ItemAID    int
	ItemBID    int
	WinnerID   *int // nil se ainda nom foi jogado
	MatchOrder int
	// Campos auxiliares para a UI
	ItemAName        string
	ItemBName        string
	ItemADescription string
	ItemBDescription string
	ItemALink        string
	ItemBLink        string
	ItemAImage       string
	ItemBImage       string
}

// VersusStanding representa a classificaçom de um elemento no torneio
type VersusStanding struct {
	SessionID int
	ItemID    int
	Wins      int
	Losses    int
	Buchholz  float64
	// Campo auxiliar
	ItemName string
}

// CalcTotalComparisons calcula o número total de comparaçons
// baseado no modo e no número de elementos.
// Fórmulas: Rápido = 1.5n + 5, Detalhado = 2n + 10
// Se n <= 5, usa round-robin (todos contra todos)
func CalcTotalComparisons(n int, mode string) (total int, isRoundRobin bool) {
	if n <= 5 {
		// Round-robin: cada par joga uma vez = n*(n-1)/2
		return n * (n - 1) / 2, true
	}

	nf := float64(n)
	switch mode {
	case "detalhado":
		total = int(math.Floor(2*nf + 10))
	default: // "rapido"
		total = int(math.Floor(1.5*nf + 5))
	}
	return total, false
}

// StartVersusSession cria uma nova sessom de Versus para uma lista.
// Gera os emparelamentos da primeira ronda e inicializa as classificaçons.
func StartVersusSession(db *sql.DB, listID int, mode string) (int, error) {
	// Obter os elementos da lista
	items, err := GetListItems(db, listID)
	if err != nil {
		return 0, err
	}

	n := len(items)
	total, isRR := CalcTotalComparisons(n, mode)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Criar a sessom
	result, err := tx.Exec(`
		INSERT INTO versus_sessions (list_id, mode, total_comparisons, is_round_robin)
		VALUES (?, ?, ?, ?)
	`, listID, mode, total, isRR)
	if err != nil {
		return 0, err
	}

	sessionID64, _ := result.LastInsertId()
	sessionID := int(sessionID64)

	// Inicializar as classificaçons (todos começam com 0 vitórias)
	for _, item := range items {
		_, err = tx.Exec(`
			INSERT INTO versus_standings (session_id, item_id, wins, losses, buchholz)
			VALUES (?, ?, 0, 0, 0)
		`, sessionID, item.ID)
		if err != nil {
			return 0, err
		}
	}

	// Gerar os emparelamentos
	if isRR {
		// Round-robin: gerar TODOS os pares possíveis
		err = generateRoundRobinMatches(tx, sessionID, items)
	} else {
		// Sistema suíço: gerar só a primeira ronda (aleatória)
		err = generateFirstRound(tx, sessionID, items)
	}
	if err != nil {
		return 0, err
	}

	return sessionID, tx.Commit()
}

// generateRoundRobinMatches gera todos os pares possíveis para round-robin.
// Usado quando há 5 ou menos elementos.
func generateRoundRobinMatches(tx *sql.Tx, sessionID int, items []ListItem) error {
	order := 1
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			_, err := tx.Exec(`
				INSERT INTO versus_matches (session_id, round, item_a_id, item_b_id, match_order)
				VALUES (?, 1, ?, ?, ?)
			`, sessionID, items[i].ID, items[j].ID, order)
			if err != nil {
				return err
			}
			order++
		}
	}
	return nil
}

// generateFirstRound gera emparelamentos aleatórios para a primeira ronda.
// Baralha os elementos e emparelha-os de dois em dois.
func generateFirstRound(tx *sql.Tx, sessionID int, items []ListItem) error {
	// Baralhar os elementos aleatoriamente
	shuffled := make([]ListItem, len(items))
	copy(shuffled, items)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Obter o último match_order existente
	order := 1

	// Emparelhar de dois em dois
	for i := 0; i+1 < len(shuffled); i += 2 {
		_, err := tx.Exec(`
			INSERT INTO versus_matches (session_id, round, item_a_id, item_b_id, match_order)
			VALUES (?, 1, ?, ?, ?)
		`, sessionID, shuffled[i].ID, shuffled[i+1].ID, order)
		if err != nil {
			return err
		}
		order++
	}
	return nil
}

// GetVersusSession obtém uma sessom de Versus pelo seu ID
func GetVersusSession(db *sql.DB, sessionID int) (*VersusSession, error) {
	s := &VersusSession{}
	err := db.QueryRow(`
		SELECT id, list_id, mode, total_comparisons, completed_comparisons,
		       is_round_robin, current_round, finished
		FROM versus_sessions WHERE id = ?
	`, sessionID).Scan(
		&s.ID, &s.ListID, &s.Mode, &s.TotalComparisons,
		&s.CompletedComparisons, &s.IsRoundRobin, &s.CurrentRound, &s.Finished,
	)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// GetNextDuel obtém o próximo enfrentamento por jogar.
// Retorna nil se todos os duelos da ronda actual estám jogados
// (nesse caso, há que gerar a ronda seguinte ou finalizar).
func GetNextDuel(db *sql.DB, sessionID int) (*VersusMatch, error) {
	m := &VersusMatch{}
	err := db.QueryRow(`
		SELECT vm.id, vm.session_id, vm.round, vm.item_a_id, vm.item_b_id,
		       vm.match_order, a.name, b.name,
		       a.description, a.link, a.image, b.description, b.link, b.image
		FROM versus_matches vm
		JOIN list_items a ON vm.item_a_id = a.id
		JOIN list_items b ON vm.item_b_id = b.id
		WHERE vm.session_id = ? AND vm.winner_id IS NULL
		ORDER BY vm.match_order
		LIMIT 1
	`, sessionID).Scan(
		&m.ID, &m.SessionID, &m.Round, &m.ItemAID, &m.ItemBID,
		&m.MatchOrder, &m.ItemAName, &m.ItemBName,
		&m.ItemADescription, &m.ItemALink, &m.ItemAImage, &m.ItemBDescription, &m.ItemBLink, &m.ItemBImage,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Nom há mais duelos pendentes
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// RecordResult regista o resultado de um enfrentamento e actualiza as classificaçons.
func RecordResult(db *sql.DB, sessionID, matchID, winnerID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Obter os dados do enfrentamento
	var itemAID, itemBID int
	err = tx.QueryRow(
		"SELECT item_a_id, item_b_id FROM versus_matches WHERE id = ? AND session_id = ?",
		matchID, sessionID,
	).Scan(&itemAID, &itemBID)
	if err != nil {
		return err
	}

	// Determinar o perdedor
	loserID := itemBID
	if winnerID == itemBID {
		loserID = itemAID
	}

	// Registar o vencedor
	_, err = tx.Exec(
		"UPDATE versus_matches SET winner_id = ? WHERE id = ?",
		winnerID, matchID,
	)
	if err != nil {
		return err
	}

	// Actualizar vitórias do vencedor
	_, err = tx.Exec(
		"UPDATE versus_standings SET wins = wins + 1 WHERE session_id = ? AND item_id = ?",
		sessionID, winnerID,
	)
	if err != nil {
		return err
	}

	// Actualizar derrotas do perdedor
	_, err = tx.Exec(
		"UPDATE versus_standings SET losses = losses + 1 WHERE session_id = ? AND item_id = ?",
		sessionID, loserID,
	)
	if err != nil {
		return err
	}

	// Incrementar comparaçons completadas
	_, err = tx.Exec(
		"UPDATE versus_sessions SET completed_comparisons = completed_comparisons + 1 WHERE id = ?",
		sessionID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GenerateNextRoundIfNeeded verifica se é preciso gerar uma nova ronda.
// Retorna true se o torneio terminou (atingiu o total de comparaçons).
func GenerateNextRoundIfNeeded(db *sql.DB, sessionID int) (bool, error) {
	session, err := GetVersusSession(db, sessionID)
	if err != nil {
		return false, err
	}

	// Se é round-robin ou já terminou, nom gerar mais rondas
	if session.IsRoundRobin || session.Finished {
		// Verificar se atingiu o total
		if session.CompletedComparisons >= session.TotalComparisons {
			db.Exec("UPDATE versus_sessions SET finished = 1 WHERE id = ?", sessionID)
			return true, nil
		}
		// Em round-robin, verificar se há mais duelos
		next, err := GetNextDuel(db, sessionID)
		if err != nil {
			return false, err
		}
		if next == nil {
			db.Exec("UPDATE versus_sessions SET finished = 1 WHERE id = ?", sessionID)
			return true, nil
		}
		return false, nil
	}

	// Verificar se atingimos o total de comparaçons
	if session.CompletedComparisons >= session.TotalComparisons {
		db.Exec("UPDATE versus_sessions SET finished = 1 WHERE id = ?", sessionID)
		return true, nil
	}

	// Verificar se há duelos pendentes na ronda actual
	next, err := GetNextDuel(db, sessionID)
	if err != nil {
		return false, err
	}
	if next != nil {
		return false, nil // Ainda há duelos por jogar nesta ronda
	}

	// Calcular quantas comparaçons faltam
	remaining := session.TotalComparisons - session.CompletedComparisons

	// Gerar a próxima ronda usando emparelamento suíço
	err = generateSwissRound(db, sessionID, session.CurrentRound+1, remaining)
	if err != nil {
		return false, err
	}

	// Actualizar a ronda actual
	_, err = db.Exec(
		"UPDATE versus_sessions SET current_round = current_round + 1 WHERE id = ?",
		sessionID,
	)
	return false, err
}

// generateSwissRound gera os emparelamentos para uma nova ronda do torneio suíço.
// Emparelha elementos com pontuaçom similar, evitando repetir enfrentamentos.
func generateSwissRound(db *sql.DB, sessionID, roundNum, maxMatches int) error {
	// Obter as classificaçons actuais, ordenadas por vitórias (descendente)
	standings, err := GetStandings(db, sessionID)
	if err != nil {
		return err
	}

	// Obter todos os enfrentamentos já realizados (para evitar repetiçons)
	played, err := getPlayedPairs(db, sessionID)
	if err != nil {
		return err
	}

	// Obter o último match_order
	var maxOrder int
	db.QueryRow(
		"SELECT COALESCE(MAX(match_order), 0) FROM versus_matches WHERE session_id = ?",
		sessionID,
	).Scan(&maxOrder)

	// Algoritmo de emparelamento suíço:
	// 1. Ordenar por vitórias (descendente)
	// 2. Tentar emparelhar o primeiro com o segundo, o terceiro com o quarto, etc.
	// 3. Se um par já jogou entre si, deslizar para baixo
	paired := make(map[int]bool) // Marcar elementos já emparelhados
	order := maxOrder + 1
	matchCount := 0

	// Calcular máximo de partidas nesta ronda (n/2, mas sem exceder o restante)
	maxInRound := len(standings) / 2
	if maxInRound > maxMatches {
		maxInRound = maxMatches
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := 0; i < len(standings) && matchCount < maxInRound; i++ {
		if paired[standings[i].ItemID] {
			continue
		}

		// Tentar emparelhar com o próximo nom emparelhado
		for j := i + 1; j < len(standings); j++ {
			if paired[standings[j].ItemID] {
				continue
			}

			// Verificar se este par já jogou
			pairKey := makePairKey(standings[i].ItemID, standings[j].ItemID)
			if played[pairKey] {
				continue // Já jogárom, tentar outro rival
			}

			// Emparelhar!
			_, err := tx.Exec(`
				INSERT INTO versus_matches (session_id, round, item_a_id, item_b_id, match_order)
				VALUES (?, ?, ?, ?, ?)
			`, sessionID, roundNum, standings[i].ItemID, standings[j].ItemID, order)
			if err != nil {
				return err
			}

			paired[standings[i].ItemID] = true
			paired[standings[j].ItemID] = true
			order++
			matchCount++
			break
		}
	}

	// Se nom conseguimos emparelhar ninguém (todos já jogárom entre si),
	// permitir repetiçons para completar a ronda
	if matchCount == 0 {
		for i := 0; i+1 < len(standings) && matchCount < maxInRound; i += 2 {
			_, err := tx.Exec(`
				INSERT INTO versus_matches (session_id, round, item_a_id, item_b_id, match_order)
				VALUES (?, ?, ?, ?, ?)
			`, sessionID, roundNum, standings[i].ItemID, standings[i+1].ItemID, order)
			if err != nil {
				return err
			}
			order++
			matchCount++
		}
	}

	return tx.Commit()
}

// GetStandings obtém as classificaçons de todos os elementos, ordenadas por vitórias e Buchholz
func GetStandings(db *sql.DB, sessionID int) ([]VersusStanding, error) {
	// Primeiro, recalcular Buchholz para todos
	if err := recalcBuchholz(db, sessionID); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT vs.session_id, vs.item_id, vs.wins, vs.losses, vs.buchholz, li.name
		FROM versus_standings vs
		JOIN list_items li ON vs.item_id = li.id
		WHERE vs.session_id = ?
		ORDER BY vs.wins DESC, vs.buchholz DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var standings []VersusStanding
	for rows.Next() {
		var s VersusStanding
		if err := rows.Scan(&s.SessionID, &s.ItemID, &s.Wins, &s.Losses, &s.Buchholz, &s.ItemName); err != nil {
			return nil, err
		}
		standings = append(standings, s)
	}
	return standings, nil
}

// recalcBuchholz recalcula a pontuaçom de Buchholz para todos os elementos.
// Buchholz = soma das vitórias de todos os rivais com quem jogou.
// Quanto maior, significa que enfrentou rivais mais fortes.
func recalcBuchholz(db *sql.DB, sessionID int) error {
	// Obter todos os enfrentamentos jogados
	rows, err := db.Query(`
		SELECT item_a_id, item_b_id, winner_id
		FROM versus_matches
		WHERE session_id = ? AND winner_id IS NOT NULL
	`, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Construir o mapa de oponentes de cada elemento
	type matchInfo struct {
		opponentID int
	}
	opponents := make(map[int][]int) // itemID -> lista de oponentes

	for rows.Next() {
		var aID, bID int
		var winnerID int
		if err := rows.Scan(&aID, &bID, &winnerID); err != nil {
			return err
		}
		opponents[aID] = append(opponents[aID], bID)
		opponents[bID] = append(opponents[bID], aID)
	}

	// Obter as vitórias de cada elemento
	winsMap := make(map[int]int)
	standingRows, err := db.Query(
		"SELECT item_id, wins FROM versus_standings WHERE session_id = ?", sessionID,
	)
	if err != nil {
		return err
	}
	defer standingRows.Close()

	for standingRows.Next() {
		var itemID, wins int
		if err := standingRows.Scan(&itemID, &wins); err != nil {
			return err
		}
		winsMap[itemID] = wins
	}

	// Calcular e actualizar Buchholz para cada elemento
	for itemID, opps := range opponents {
		var buchholz float64
		for _, oppID := range opps {
			buchholz += float64(winsMap[oppID])
		}
		_, err := db.Exec(
			"UPDATE versus_standings SET buchholz = ? WHERE session_id = ? AND item_id = ?",
			buchholz, sessionID, itemID,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// getPlayedPairs obtém todos os pares que já jogárom entre si.
// Retorna um mapa de "ID_menor-ID_maior" -> true
func getPlayedPairs(db *sql.DB, sessionID int) (map[string]bool, error) {
	rows, err := db.Query(
		"SELECT item_a_id, item_b_id FROM versus_matches WHERE session_id = ?",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pairs := make(map[string]bool)
	for rows.Next() {
		var a, b int
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		pairs[makePairKey(a, b)] = true
	}
	return pairs, nil
}

// makePairKey cria uma chave única para um par de elementos.
// Usa sempre o ID menor primeiro para garantir consistência.
func makePairKey(a, b int) string {
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("%d-%d", a, b)
}

// UndoLastMatch desfaz o último enfrentamento jogado.
// Reverte o resultado e as classificaçons.
func UndoLastMatch(db *sql.DB, sessionID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Encontrar o último enfrentamento jogado
	var matchID, itemAID, itemBID, winnerID int
	err = tx.QueryRow(`
		SELECT id, item_a_id, item_b_id, winner_id
		FROM versus_matches
		WHERE session_id = ? AND winner_id IS NOT NULL
		ORDER BY match_order DESC
		LIMIT 1
	`, sessionID).Scan(&matchID, &itemAID, &itemBID, &winnerID)
	if err != nil {
		return err // Nom há nada para desfazer
	}

	// Determinar o perdedor
	loserID := itemBID
	if winnerID == itemBID {
		loserID = itemAID
	}

	// Limpar o resultado do enfrentamento
	_, err = tx.Exec("UPDATE versus_matches SET winner_id = NULL WHERE id = ?", matchID)
	if err != nil {
		return err
	}

	// Reverter vitórias e derrotas
	_, err = tx.Exec(
		"UPDATE versus_standings SET wins = wins - 1 WHERE session_id = ? AND item_id = ?",
		sessionID, winnerID,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		"UPDATE versus_standings SET losses = losses - 1 WHERE session_id = ? AND item_id = ?",
		sessionID, loserID,
	)
	if err != nil {
		return err
	}

	// Decrementar comparaçons completadas
	_, err = tx.Exec(
		"UPDATE versus_sessions SET completed_comparisons = completed_comparisons - 1, finished = 0 WHERE id = ?",
		sessionID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ApplyVersusResults aplica o resultado do torneio à lista.
// Ordena os elementos segundo as classificaçons finais e actualiza as posiçons.
func ApplyVersusResults(db *sql.DB, sessionID int) error {
	session, err := GetVersusSession(db, sessionID)
	if err != nil {
		return err
	}

	// Obter classificaçons finais (já ordenadas por vitórias + Buchholz)
	standings, err := GetStandings(db, sessionID)
	if err != nil {
		return err
	}

	// Actualizar as posiçons na lista
	itemIDs := make([]int, len(standings))
	for i, s := range standings {
		itemIDs[i] = s.ItemID
	}

	return UpdateItemPositions(db, session.ListID, itemIDs)
}

// makePairKeyInt cria uma chave numérica para ordenaçom
func makePairKeyInt(a, b int) int64 {
	if a > b {
		a, b = b, a
	}
	return int64(a)*1000000 + int64(b)
}

// sortStandings ordena standings por vitórias (desc) e Buchholz (desc)
func sortStandings(standings []VersusStanding) {
	sort.Slice(standings, func(i, j int) bool {
		if standings[i].Wins != standings[j].Wins {
			return standings[i].Wins > standings[j].Wins
		}
		return standings[i].Buchholz > standings[j].Buchholz
	})
}
