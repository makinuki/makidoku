-- Clearance material for one source and one registrable domain, captured
-- together from a single solve. Expiry is advisory: Cloudflare enforces the
-- zone's challenge passage server-side, so an observed expiry can disagree
-- with the effective boundary in either direction.
--
-- Keyed by origin rather than source alone. Clearance for one registrable
-- domain is never sent to another, so a source whose pages and images sit on
-- different domains keeps one row per domain.
CREATE TABLE IF NOT EXISTS clearance_bundle (
    source_id         TEXT    NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    origin            TEXT    NOT NULL,
    -- cookies is a JSON object of the whole jar, not only cf_clearance.
    -- Dropping companion cookies such as __cf_bm causes an immediate
    -- re-challenge on many zones.
    cookies           TEXT    NOT NULL,
    -- user_agent and sec_ch_ua are captured in the same solve as the cookies.
    -- A cookie is only valid for the client identity that obtained it.
    user_agent        TEXT    NOT NULL,
    sec_ch_ua         TEXT,
    -- browser_profile ties the record to a transport profile so a profile
    -- change invalidates the bundle rather than failing silently.
    browser_profile   TEXT    NOT NULL DEFAULT 'default',
    obtained_at       INTEGER NOT NULL,
    -- expires_hint is advisory and for display only.
    expires_hint      INTEGER,
    last_success_at   INTEGER,
    last_challenge_at INTEGER,
    -- generation increments on each solve. Concurrent requests that fail with a
    -- challenge sent under the same generation are one event, so the bundle is
    -- invalidated and re-solved once rather than per request.
    generation        INTEGER NOT NULL DEFAULT 1,
    -- status is one of unknown, usable, challenged, invalid.
    status            TEXT    NOT NULL DEFAULT 'unknown',
    PRIMARY KEY (source_id, origin)
);

-- Origins are looked up on every request that touches a source.
CREATE INDEX IF NOT EXISTS clearance_bundle_source_idx ON clearance_bundle(source_id);