package models

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// VersusSession represents an ongoing Swiss-tournament session.
type VersusSession struct {
	ID                   int
	ListID               int
	Mode                 string // "rapido" or "detalhado"
	TotalComparisons     int
	CompletedComparisons int
	IsRoundRobin         bool
	CurrentRound         int
	Finished             bool
}

// VersusMatch represents an individual head-to-head match between two items.
type VersusMatch struct {
	ID         int
	SessionID  int
	Round      int
	ItemAID    int
	ItemBID    int
	WinnerID   *int // nil if the match has not been played yet
	MatchOrder int
	// Auxiliary fields for the UI
	ItemAName        string
	ItemBName        string
	ItemADescription string
	ItemBDescription string
	ItemALink        string
	ItemBLink        string
	ItemAImage       string
	ItemBImage       string
}

// VersusStanding represents a single item's standing in the tournament.
type VersusStanding struct {
	SessionID int
	ItemID    int
	Wins      int
	Losses    int
	Buchholz  float64
	// Auxiliary field
	ItemName string
}

// CalcTotalComparisons calculates the total number of comparisons
// based on the mode and number of items.
// Formulas: Quick = 1.5n + 5, Detailed = 2n + 10
// If n <= 5, uses round-robin (every pair plays once).
func CalcTotalComparisons(n int, mode string) (total int, isRoundRobin bool) {
	if n <= 5 {
		// Round-robin: each pair plays once = n*(n-1)/2
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

// StartVersusSession creates a new Versus session for a list.
// Generates the first-round pairings and initialises standings.
func StartVersusSession(db *sql.DB, listID int, mode string) (int, error) {
	// Fetch list items
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

	// Create the session
	result, err := tx.Exec(`
		INSERT INTO versus_sessions (list_id, mode, total_comparisons, is_round_robin)
		VALUES (?, ?, ?, ?)
	`, listID, mode, total, isRR)
	if err != nil {
		return 0, err
	}

	sessionID64, _ := result.LastInsertId()
	sessionID := int(sessionID64)

	// Initialise standings (everyone starts with 0 wins)
	for _, item := range items {
		_, err = tx.Exec(`
			INSERT INTO versus_standings (session_id, item_id, wins, losses, buchholz)
			VALUES (?, ?, 0, 0, 0)
		`, sessionID, item.ID)
		if err != nil {
			return 0, err
		}
	}

	// Generate pairings
	if isRR {
		// Round-robin: generate ALL possible pairs upfront
		err = generateRoundRobinMatches(tx, sessionID, items)
	} else {
		// Swiss system: generate only the first round (random pairings)
		err = generateFirstRound(tx, sessionID, items)
	}
	if err != nil {
		return 0, err
	}

	return sessionID, tx.Commit()
}

// generateRoundRobinMatches generates all possible pairs for round-robin mode.
// Used when there are 5 or fewer items.
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

// generateFirstRound generates random pairings for the first round.
// Shuffles items and pairs them two-by-two.
func generateFirstRound(tx *sql.Tx, sessionID int, items []ListItem) error {
	// Shuffle items randomly
	shuffled := make([]ListItem, len(items))
	copy(shuffled, items)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	order := 1

	// Pair items two by two
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

// GetVersusSession retrieves a Versus session by its ID.
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

// GetNextDuel returns the next unplayed match in the current round.
// Returns nil if all matches in the current round have been played
// (in that case, a new round should be generated or the session finalised).
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
		return nil, nil // No pending matches
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// RecordResult records the outcome of a match and updates standings.
func RecordResult(db *sql.DB, sessionID, matchID, winnerID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Fetch match data
	var itemAID, itemBID int
	err = tx.QueryRow(
		"SELECT item_a_id, item_b_id FROM versus_matches WHERE id = ? AND session_id = ?",
		matchID, sessionID,
	).Scan(&itemAID, &itemBID)
	if err != nil {
		return err
	}

	// Determine the loser
	loserID := itemBID
	if winnerID == itemBID {
		loserID = itemAID
	}

	// Record the winner
	_, err = tx.Exec(
		"UPDATE versus_matches SET winner_id = ? WHERE id = ?",
		winnerID, matchID,
	)
	if err != nil {
		return err
	}

	// Increment winner's win count
	_, err = tx.Exec(
		"UPDATE versus_standings SET wins = wins + 1 WHERE session_id = ? AND item_id = ?",
		sessionID, winnerID,
	)
	if err != nil {
		return err
	}

	// Increment loser's loss count
	_, err = tx.Exec(
		"UPDATE versus_standings SET losses = losses + 1 WHERE session_id = ? AND item_id = ?",
		sessionID, loserID,
	)
	if err != nil {
		return err
	}

	// Increment completed comparisons counter
	_, err = tx.Exec(
		"UPDATE versus_sessions SET completed_comparisons = completed_comparisons + 1 WHERE id = ?",
		sessionID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GenerateNextRoundIfNeeded checks whether a new round needs to be generated.
// Returns true if the tournament has ended (total comparisons reached).
func GenerateNextRoundIfNeeded(db *sql.DB, sessionID int) (bool, error) {
	session, err := GetVersusSession(db, sessionID)
	if err != nil {
		return false, err
	}

	// Round-robin or already finished: no more rounds to generate
	if session.IsRoundRobin || session.Finished {
		// Check if total comparisons reached
		if session.CompletedComparisons >= session.TotalComparisons {
			db.Exec("UPDATE versus_sessions SET finished = 1 WHERE id = ?", sessionID)
			return true, nil
		}
		// In round-robin, check if any matches remain
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

	// Check if total comparisons reached
	if session.CompletedComparisons >= session.TotalComparisons {
		db.Exec("UPDATE versus_sessions SET finished = 1 WHERE id = ?", sessionID)
		return true, nil
	}

	// Check if there are pending matches in the current round
	next, err := GetNextDuel(db, sessionID)
	if err != nil {
		return false, err
	}
	if next != nil {
		return false, nil // Matches still pending in this round
	}

	// Calculate how many comparisons remain
	remaining := session.TotalComparisons - session.CompletedComparisons

	// Generate the next round using Swiss pairings
	err = generateSwissRound(db, sessionID, session.CurrentRound+1, remaining)
	if err != nil {
		return false, err
	}

	// Advance to the next round
	_, err = db.Exec(
		"UPDATE versus_sessions SET current_round = current_round + 1 WHERE id = ?",
		sessionID,
	)
	return false, err
}

// generateSwissRound generates pairings for a new Swiss-tournament round.
// Matches items with similar scores, avoiding rematches wherever possible.
func generateSwissRound(db *sql.DB, sessionID, roundNum, maxMatches int) error {
	// Fetch current standings ordered by wins (descending)
	standings, err := GetStandings(db, sessionID)
	if err != nil {
		return err
	}

	// Fetch all already-played pairs (to avoid rematches)
	played, err := getPlayedPairs(db, sessionID)
	if err != nil {
		return err
	}

	// Fetch the current highest match_order
	var maxOrder int
	db.QueryRow(
		"SELECT COALESCE(MAX(match_order), 0) FROM versus_matches WHERE session_id = ?",
		sessionID,
	).Scan(&maxOrder)

	// Swiss pairing algorithm:
	// 1. Sort by wins (descending)
	// 2. Try to pair 1st with 2nd, 3rd with 4th, etc.
	// 3. If a pair has already played, slide down to find an alternative
	paired := make(map[int]bool) // track already-paired items
	order := maxOrder + 1
	matchCount := 0

	// Cap matches per round at n/2, but never exceed remaining budget
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

		// Try to pair with the next unpaired item
		for j := i + 1; j < len(standings); j++ {
			if paired[standings[j].ItemID] {
				continue
			}

			// Skip pairs that have already played each other
			pairKey := makePairKey(standings[i].ItemID, standings[j].ItemID)
			if played[pairKey] {
				continue
			}

			// Pair them!
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

	// If no pairings were made (all possible pairs have already played),
	// allow rematches to complete the round
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

// GetStandings retrieves all items' standings ordered by wins and Buchholz score.
func GetStandings(db *sql.DB, sessionID int) ([]VersusStanding, error) {
	// Recalculate Buchholz scores first
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

// recalcBuchholz recalculates the Buchholz score for all items.
// Buchholz = sum of the wins of all opponents faced.
// A higher score indicates stronger opponents were beaten.
func recalcBuchholz(db *sql.DB, sessionID int) error {
	// Fetch all played matches
	rows, err := db.Query(`
		SELECT item_a_id, item_b_id, winner_id
		FROM versus_matches
		WHERE session_id = ? AND winner_id IS NOT NULL
	`, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Build a map of opponents for each item
	type matchInfo struct {
		opponentID int
	}
	opponents := make(map[int][]int) // itemID -> list of opponents

	for rows.Next() {
		var aID, bID int
		var winnerID int
		if err := rows.Scan(&aID, &bID, &winnerID); err != nil {
			return err
		}
		opponents[aID] = append(opponents[aID], bID)
		opponents[bID] = append(opponents[bID], aID)
	}

	// Fetch win counts for each item
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

	// Calculate and store Buchholz for each item
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

// getPlayedPairs returns all pairs that have already faced each other.
// Returns a map of "smaller_ID-larger_ID" -> true.
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

// makePairKey creates a unique string key for a pair of items.
// Always uses the smaller ID first to guarantee consistency.
func makePairKey(a, b int) string {
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("%d-%d", a, b)
}

// UndoLastMatch reverts the last played match and its effect on standings.
func UndoLastMatch(db *sql.DB, sessionID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Find the last played match
	var matchID, itemAID, itemBID, winnerID int
	err = tx.QueryRow(`
		SELECT id, item_a_id, item_b_id, winner_id
		FROM versus_matches
		WHERE session_id = ? AND winner_id IS NOT NULL
		ORDER BY match_order DESC
		LIMIT 1
	`, sessionID).Scan(&matchID, &itemAID, &itemBID, &winnerID)
	if err != nil {
		return err // Nothing to undo
	}

	// Determine the loser
	loserID := itemBID
	if winnerID == itemBID {
		loserID = itemAID
	}

	// Clear the match result
	_, err = tx.Exec("UPDATE versus_matches SET winner_id = NULL WHERE id = ?", matchID)
	if err != nil {
		return err
	}

	// Revert wins and losses
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

	// Decrement completed comparisons counter
	_, err = tx.Exec(
		"UPDATE versus_sessions SET completed_comparisons = completed_comparisons - 1, finished = 0 WHERE id = ?",
		sessionID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ApplyVersusResults applies the tournament result to the list.
// Orders items according to the final standings and updates their positions.
func ApplyVersusResults(db *sql.DB, sessionID int) error {
	session, err := GetVersusSession(db, sessionID)
	if err != nil {
		return err
	}

	// Fetch final standings (already sorted by wins + Buchholz)
	standings, err := GetStandings(db, sessionID)
	if err != nil {
		return err
	}

	// Update item positions in the list
	itemIDs := make([]int, len(standings))
	for i, s := range standings {
		itemIDs[i] = s.ItemID
	}

	return UpdateItemPositions(db, session.ListID, itemIDs)
}

// makePairKeyInt creates a numeric key for ordering purposes.
func makePairKeyInt(a, b int) int64 {
	if a > b {
		a, b = b, a
	}
	return int64(a)*1000000 + int64(b)
}

// sortStandings sorts standings by wins (desc) then Buchholz (desc).
func sortStandings(standings []VersusStanding) {
	sort.Slice(standings, func(i, j int) bool {
		if standings[i].Wins != standings[j].Wins {
			return standings[i].Wins > standings[j].Wins
		}
		return standings[i].Buchholz > standings[j].Buchholz
	})
}
