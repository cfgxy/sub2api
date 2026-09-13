DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'batch_image_jobs'
          AND column_name = 'enterprise_attribution_snapshot'
    ) THEN
        -- Keep the check and DROP COLUMN under one table lock. A worker that
        -- started before this lock either commits first and is observed below,
        -- or waits until the rollback has finished.
        LOCK TABLE batch_image_jobs IN ACCESS EXCLUSIVE MODE;
        IF EXISTS (
            SELECT 1
            FROM batch_image_jobs
            WHERE enterprise_attribution_snapshot IS NOT NULL
            LIMIT 1
        ) THEN
            RAISE EXCEPTION 'cannot rollback SHAN-153 batch image attribution while frozen snapshots exist';
        END IF;
        EXECUTE 'ALTER TABLE batch_image_jobs DROP COLUMN enterprise_attribution_snapshot';
    END IF;

    DELETE FROM schema_migrations
    WHERE filename = '241_batch_image_enterprise_attribution_snapshot.sql';
END;
$$;
