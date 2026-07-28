-- Static visual placement for clips: canonical ClipPlacement JSON, NULL means
-- identity. Read and written only through the clip state loader and projection
-- writer; the render plan compiler consumes it through ClipState.
ALTER TABLE clips ADD COLUMN placement TEXT;
