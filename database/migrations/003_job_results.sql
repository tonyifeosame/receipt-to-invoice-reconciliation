-- Outcome of a job. For OCR_PROCESS this is the complete OCR document and the
-- company API's reply, or the error that stopped the attempt. It is written on
-- every attempt, so a job that fails at the company API still keeps what OCR
-- extracted. See backend/jobs/ocr_job.go.
ALTER TABLE job_queue ADD COLUMN IF NOT EXISTS result JSONB;
