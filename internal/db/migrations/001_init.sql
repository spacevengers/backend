CREATE TABLE ingestion_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_uid TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    trigger TEXT NOT NULL DEFAULT 'cli',
    source TEXT NOT NULL,
    sensor TEXT NOT NULL,
    product TEXT,
    processing TEXT,
    area TEXT NOT NULL,
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    requested_at INTEGER NOT NULL,
    started_at INTEGER,
    completed_at INTEGER,
    fetched_count INTEGER NOT NULL DEFAULT 0,
    inserted_count INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    rejected_count INTEGER NOT NULL DEFAULT 0,
    error TEXT,
    errors TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX idx_ingestion_runs_status ON ingestion_runs(status);
CREATE INDEX idx_ingestion_runs_source ON ingestion_runs(source);
CREATE INDEX idx_ingestion_runs_fingerprint ON ingestion_runs(request_fingerprint);

CREATE TABLE observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    observation_uid TEXT NOT NULL UNIQUE,
    sensor TEXT NOT NULL,
    satellite TEXT NOT NULL,
    satellite_raw TEXT,
    source TEXT NOT NULL,
    source_product TEXT NOT NULL,
    source_version TEXT,
    processing TEXT,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    acquired_at INTEGER NOT NULL,
    source_date TEXT NOT NULL,
    source_time TEXT NOT NULL,
    confidence_raw TEXT,
    confidence_numeric REAL,
    confidence_class TEXT,
    confidence_scheme TEXT,
    frp REAL,
    frp_units TEXT NOT NULL DEFAULT 'MW',
    brightness REAL,
    brightness_aux REAL,
    scan REAL,
    track REAL,
    daynight TEXT,
    fire_type INTEGER,
    raw TEXT NOT NULL DEFAULT '{}',
    first_ingestion_run_id INTEGER REFERENCES ingestion_runs(id) ON DELETE SET NULL,
    last_ingestion_run_id INTEGER REFERENCES ingestion_runs(id) ON DELETE SET NULL,
    first_seen_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    ingested_at INTEGER NOT NULL
);

CREATE INDEX idx_observations_sensor ON observations(sensor);
CREATE INDEX idx_observations_satellite ON observations(satellite);
CREATE INDEX idx_observations_source ON observations(source);
CREATE INDEX idx_observations_acquired_at ON observations(acquired_at);
CREATE INDEX idx_observations_confidence_class ON observations(confidence_class);
CREATE INDEX idx_observations_bbox ON observations(latitude, longitude);
CREATE INDEX idx_observations_sensor_time ON observations(sensor, acquired_at);

CREATE TABLE harmonization_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_uid TEXT NOT NULL UNIQUE,
    method TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    params TEXT NOT NULL DEFAULT '{}',
    filters TEXT NOT NULL DEFAULT '{}',
    started_at INTEGER,
    completed_at INTEGER,
    input_count INTEGER NOT NULL DEFAULT 0,
    cluster_count INTEGER NOT NULL DEFAULT 0,
    match_count INTEGER NOT NULL DEFAULT 0,
    metrics TEXT NOT NULL DEFAULT '{}',
    error TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_harmonization_runs_status ON harmonization_runs(status);
CREATE INDEX idx_harmonization_runs_method ON harmonization_runs(method);

CREATE TABLE harmonized_observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster_uid TEXT NOT NULL UNIQUE,
    run_id INTEGER NOT NULL REFERENCES harmonization_runs(id) ON DELETE CASCADE,
    centroid_latitude REAL NOT NULL,
    centroid_longitude REAL NOT NULL,
    start_time INTEGER NOT NULL,
    end_time INTEGER NOT NULL,
    observation_count INTEGER NOT NULL DEFAULT 0,
    modis_count INTEGER NOT NULL DEFAULT 0,
    viirs_count INTEGER NOT NULL DEFAULT 0,
    satellites TEXT NOT NULL DEFAULT '[]',
    total_frp REAL,
    mean_frp REAL,
    confidence_summary TEXT NOT NULL DEFAULT '{}',
    uncertainty TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_harmonized_run ON harmonized_observations(run_id);
CREATE INDEX idx_harmonized_start_time ON harmonized_observations(start_time);
CREATE INDEX idx_harmonized_bbox ON harmonized_observations(centroid_latitude, centroid_longitude);

CREATE TABLE harmonized_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    harmonized_id INTEGER NOT NULL REFERENCES harmonized_observations(id) ON DELETE CASCADE,
    observation_id INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    UNIQUE(harmonized_id, observation_id)
);

CREATE INDEX idx_harmonized_members_harmonized ON harmonized_members(harmonized_id);
CREATE INDEX idx_harmonized_members_observation ON harmonized_members(observation_id);

CREATE TABLE observation_matches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES harmonization_runs(id) ON DELETE CASCADE,
    modis_observation_id INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    viirs_observation_id INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    distance_km REAL NOT NULL,
    time_delta_minutes REAL NOT NULL,
    method TEXT NOT NULL DEFAULT 'baseline',
    created_at INTEGER NOT NULL,
    UNIQUE(run_id, modis_observation_id, viirs_observation_id)
);

CREATE INDEX idx_matches_run ON observation_matches(run_id);
CREATE INDEX idx_matches_run_distance ON observation_matches(run_id, distance_km);
CREATE INDEX idx_matches_modis ON observation_matches(modis_observation_id);
CREATE INDEX idx_matches_viirs ON observation_matches(viirs_observation_id);

CREATE TABLE export_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    export_uid TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    dataset TEXT NOT NULL,
    format TEXT NOT NULL,
    filters TEXT NOT NULL DEFAULT '{}',
    file_path TEXT,
    row_count INTEGER NOT NULL DEFAULT 0,
    error TEXT,
    created_at INTEGER NOT NULL,
    completed_at INTEGER
);

CREATE INDEX idx_export_jobs_status ON export_jobs(status);
