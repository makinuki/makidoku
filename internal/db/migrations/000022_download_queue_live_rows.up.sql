-- The download queue holds only rows that can still make progress: a finished
-- chapter is removed as it completes and cancelling removes a row outright.
-- Rows left behind by earlier versions are dropped here, so a queue snapshot
-- does not carry finished work forever.
DELETE FROM download_queue WHERE status IN ('COMPLETED', 'CANCELED');