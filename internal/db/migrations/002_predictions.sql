-- ML prediction fields. Baseline runs leave these NULL; an external
-- harmonizer or a batch prediction import fills them.
ALTER TABLE harmonized_observations ADD COLUMN p_modis_detect REAL;
ALTER TABLE harmonized_observations ADD COLUMN frp_calibrated REAL;
ALTER TABLE harmonized_observations ADD COLUMN prediction_model TEXT;
