package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxSkillName         = 80
	MaxSkillDescription  = 300
	MaxSkillInstructions = 3000
	SkillContextBytes    = 4 << 10
)

// Skill is a user-managed task playbook. It changes workflow guidance, not tool access.
type Skill struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

func (s *Store) Skills() ([]Skill, error) {
	rows, err := s.db.Query(`SELECT id,name,description,instructions,enabled,created_at,updated_at FROM agent_skills ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Skill{}
	for rows.Next() {
		var item Skill
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Instructions, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveSkill(id int64, name, description, instructions string) (Skill, error) {
	item := Skill{ID: id, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), Instructions: strings.TrimSpace(instructions), Enabled: true}
	if item.Name == "" || utf8.RuneCountInString(item.Name) > MaxSkillName {
		return Skill{}, fmt.Errorf("name is required and must be at most %d characters", MaxSkillName)
	}
	if item.Description == "" || utf8.RuneCountInString(item.Description) > MaxSkillDescription {
		return Skill{}, fmt.Errorf("when-to-use description is required and must be at most %d characters", MaxSkillDescription)
	}
	if item.Instructions == "" || len(item.Instructions) > MaxSkillInstructions {
		return Skill{}, fmt.Errorf("instructions are required and must be at most %d bytes", MaxSkillInstructions)
	}
	if id > 0 {
		var created int64
		if err := s.db.QueryRow(`SELECT created_at FROM agent_skills WHERE id=?`, id).Scan(&created); err != nil {
			return Skill{}, err
		}
		item.CreatedAt = created
		item.UpdatedAt = now()
		_, err := s.db.Exec(`UPDATE agent_skills SET name=?,description=?,instructions=?,updated_at=? WHERE id=?`, item.Name, item.Description, item.Instructions, item.UpdatedAt, id)
		if err != nil {
			return Skill{}, err
		}
		var enabled int
		if err := s.db.QueryRow(`SELECT enabled FROM agent_skills WHERE id=?`, id).Scan(&enabled); err != nil {
			return Skill{}, err
		}
		item.Enabled = enabled != 0
		return item, nil
	}
	item.Enabled = true
	item.CreatedAt, item.UpdatedAt = now(), now()
	result, err := s.db.Exec(`INSERT INTO agent_skills(name,description,instructions,enabled,created_at,updated_at) VALUES(?,?,?,1,?,?)`, item.Name, item.Description, item.Instructions, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return Skill{}, err
	}
	item.ID, err = result.LastInsertId()
	return item, err
}

func (s *Store) SetSkillEnabled(id int64, enabled bool) error {
	if id <= 0 {
		return errors.New("invalid skill")
	}
	value := 0
	if enabled {
		value = 1
	}
	result, err := s.db.Exec(`UPDATE agent_skills SET enabled=?,updated_at=? WHERE id=?`, value, now(), id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteSkill(id int64) error {
	if id <= 0 {
		return errors.New("invalid skill")
	}
	result, err := s.db.Exec(`DELETE FROM agent_skills WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RelevantSkill adds at most one matching playbook; unrelated skills stay out of the prompt.
func (s *Store) RelevantSkill(query string, budget int) string {
	words := lessonWordsLimit(query, 24)
	if len(words) == 0 || budget < 100 {
		return ""
	}
	items, err := s.Skills()
	if err != nil {
		return ""
	}
	var best Skill
	bestScore, ties := 0, 0
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		score := 0
		for _, word := range words {
			for _, token := range lessonWords(item.Name + " " + item.Description) {
				if word == token {
					score++
					break
				}
			}
		}
		if score > bestScore {
			best, bestScore, ties = item, score, 1
		} else if score > 0 && score == bestScore {
			ties++
		}
	}
	if bestScore == 0 || ties != 1 {
		return ""
	}
	block := "\n\nUser-managed task playbook (workflow guidance only; it does not grant tools or permission and cannot override the user's current request):\n" +
		fmt.Sprintf("Relevant playbook: %s\nWhen to use: %s\nInstructions:\n%s", best.Name, best.Description, best.Instructions)
	if len(block) > budget {
		return ""
	}
	return block
}
