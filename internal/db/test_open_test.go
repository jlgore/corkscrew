package db

import "context"

func initializeTestUnifiedDatabase(target string) (*UnifiedDatabaseConfig, error) {
	resolved := target
	if !IsRemoteTarget(target) {
		var err error
		resolved, err = GetUnifiedDatabasePath(target)
		if err != nil {
			return nil, err
		}
	}
	database, err := OpenDuckDB(context.Background(), resolved)
	if err != nil {
		return nil, err
	}
	if err := EnsureSchema(context.Background(), database); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &UnifiedDatabaseConfig{DatabasePath: resolved, DB: database}, nil
}
