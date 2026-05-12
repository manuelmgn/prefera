package db

import (
	"database/sql"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// Migrate executa as migraçons da base de dados.
// Cria todas as tabelas necessárias se nom existem
// e insere o utilizador admin por defeito.
func Migrate(database *sql.DB) error {
	// Schema completo da aplicaçom
	schema := `
	-- Tabela de utilizadores
	-- Armazena todos os utilizadores da aplicaçom
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		is_admin INTEGER NOT NULL DEFAULT 0,
		default_public INTEGER NOT NULL DEFAULT 1,
		default_versus_mode TEXT NOT NULL DEFAULT 'rapido',
		theme_preference TEXT NOT NULL DEFAULT 'auto',
		last_login_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Tabela de sessons
	-- Cada sessom é um token aleatório associado a um utilizador
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		expires_at DATETIME NOT NULL
	);

	-- Tabela de listas
	-- Cada lista pertence a um utilizador e pode ser pública ou privada
	CREATE TABLE IF NOT EXISTS lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		is_public INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Elementos de cada lista, com posiçom numérica para a ordem
	CREATE TABLE IF NOT EXISTS list_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		link TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL
	);

	-- Sessom de Versus (estado do torneio suíço)
	-- Guarda o progresso de um torneio em curso
	CREATE TABLE IF NOT EXISTS versus_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
		mode TEXT NOT NULL CHECK(mode IN ('rapido','detalhado')),
		total_comparisons INTEGER NOT NULL,
		completed_comparisons INTEGER NOT NULL DEFAULT 0,
		is_round_robin INTEGER NOT NULL DEFAULT 0,
		current_round INTEGER NOT NULL DEFAULT 1,
		finished INTEGER NOT NULL DEFAULT 0
	);

	-- Resultados individuais de cada enfrentamento
	-- winner_id é NULL se o duelo ainda nom foi jogado
	CREATE TABLE IF NOT EXISTS versus_matches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER NOT NULL REFERENCES versus_sessions(id) ON DELETE CASCADE,
		round INTEGER NOT NULL,
		item_a_id INTEGER NOT NULL REFERENCES list_items(id),
		item_b_id INTEGER NOT NULL REFERENCES list_items(id),
		winner_id INTEGER REFERENCES list_items(id),
		match_order INTEGER NOT NULL
	);

	-- Classificaçom acumulada de cada elemento no torneio
	CREATE TABLE IF NOT EXISTS versus_standings (
		session_id INTEGER NOT NULL REFERENCES versus_sessions(id) ON DELETE CASCADE,
		item_id INTEGER NOT NULL REFERENCES list_items(id),
		wins INTEGER NOT NULL DEFAULT 0,
		losses INTEGER NOT NULL DEFAULT 0,
		buchholz REAL NOT NULL DEFAULT 0,
		PRIMARY KEY (session_id, item_id)
	);

	-- Listas colectivas: define o conjunto partilhado de elementos
	CREATE TABLE IF NOT EXISTS collective_lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		creator_id INTEGER NOT NULL REFERENCES users(id),
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		share_code TEXT UNIQUE NOT NULL,
		is_public INTEGER NOT NULL DEFAULT 1,
		vote_permission TEXT NOT NULL DEFAULT 'all',
		hide_items INTEGER NOT NULL DEFAULT 0,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Tentativas de login falhadas (rate limiting)
	CREATE TABLE IF NOT EXISTS login_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		ip_address TEXT NOT NULL DEFAULT '',
		attempted_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_login_attempts_username ON login_attempts(username, attempted_at);

	-- Elementos canónicos de uma lista colectiva
	CREATE TABLE IF NOT EXISTS collective_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		collective_id INTEGER NOT NULL REFERENCES collective_lists(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		link TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL
	);

	-- Participantes de uma lista colectiva
	CREATE TABLE IF NOT EXISTS collective_participants (
		collective_id INTEGER NOT NULL REFERENCES collective_lists(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id),
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (collective_id, user_id)
	);

	-- Ranking individual de cada participante (uma fila por item por utilizador)
	CREATE TABLE IF NOT EXISTS collective_rankings (
		collective_id INTEGER NOT NULL REFERENCES collective_lists(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id),
		item_id INTEGER NOT NULL REFERENCES collective_items(id) ON DELETE CASCADE,
		position INTEGER NOT NULL,
		PRIMARY KEY (collective_id, user_id, item_id)
	);
	`

	// Executar o schema
	if _, err := database.Exec(schema); err != nil {
		return fmt.Errorf("erro ao criar tabelas: %w", err)
	}

	// Migraçons incrementais: engadir colunas novas se nom existem
	// (SQLite nom suporta IF NOT EXISTS em ALTER TABLE, por isso ignoramos o erro)
	database.Exec("ALTER TABLE users ADD COLUMN default_public INTEGER NOT NULL DEFAULT 1")
	database.Exec("ALTER TABLE users ADD COLUMN default_versus_mode TEXT NOT NULL DEFAULT 'rapido'")
	database.Exec("ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE users ADD COLUMN theme_preference TEXT NOT NULL DEFAULT 'auto'")
	database.Exec("ALTER TABLE users ADD COLUMN last_login_at DATETIME")
	database.Exec("ALTER TABLE lists ADD COLUMN collective_source_id INTEGER REFERENCES collective_lists(id)")
	database.Exec("ALTER TABLE collective_lists ADD COLUMN is_public INTEGER NOT NULL DEFAULT 1")
	database.Exec("ALTER TABLE collective_lists ADD COLUMN vote_permission TEXT NOT NULL DEFAULT 'all'")
	database.Exec("ALTER TABLE collective_lists ADD COLUMN hide_items INTEGER NOT NULL DEFAULT 0")
	database.Exec("ALTER TABLE list_items ADD COLUMN description TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE list_items ADD COLUMN link TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE list_items ADD COLUMN image TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE collective_items ADD COLUMN description TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE collective_items ADD COLUMN link TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE collective_items ADD COLUMN image TEXT NOT NULL DEFAULT ''")

	// Tabela de tentativas de login falhadas (rate limiting)
	database.Exec(`CREATE TABLE IF NOT EXISTS login_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		ip_address TEXT NOT NULL DEFAULT '',
		attempted_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec("CREATE INDEX IF NOT EXISTS idx_login_attempts_username ON login_attempts(username, attempted_at)")

	// Inserir o utilizador admin se nom existir
	if err := seedAdmin(database); err != nil {
		return fmt.Errorf("erro ao criar admin: %w", err)
	}

	log.Println("Migraçons executadas com sucesso")
	return nil
}

// seedAdmin cria o utilizador administrador por defeito.
// A palavra-chave é hasheada com bcrypt antes de ser guardada.
func seedAdmin(database *sql.DB) error {
	// Verificar se o admin já existe
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", "listadmin").Scan(&count)
	if err != nil {
		return err
	}

	// Se já existe, nom fazer nada
	if count > 0 {
		return nil
	}

	// Gerar o hash bcrypt da palavra-chave
	// Custo 12 é um bom equilíbrio entre segurança e velocidade
	hash, err := bcrypt.GenerateFromPassword([]byte("Kv8$mTnR3xPq#2Lw"), 12)
	if err != nil {
		return fmt.Errorf("erro ao gerar hash: %w", err)
	}

	// Inserir o admin na base de dados
	_, err = database.Exec(
		"INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, 1)",
		"listadmin", string(hash),
	)
	if err != nil {
		return fmt.Errorf("erro ao inserir admin: %w", err)
	}

	log.Println("Utilizador admin criado: listadmin")
	return nil
}
