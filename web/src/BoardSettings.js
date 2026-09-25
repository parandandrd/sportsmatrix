import React, { useCallback, useEffect, useState } from 'react';
import { CallRPC } from './util';
import { DelayChoices } from './delay';
import { ParseLocation } from './location';

function TeamPicker({ label, values, teams, allowAll, onChange }) {
    const [other, setOther] = useState('');
    const all = values.length === 1 && values[0] === 'ALL';
    const name = (abbrev) => (teams.find((t) => t.abbreviation === abbrev) || {}).name;

    const add = (team) => {
        if (!team || values.includes(team)) {
            return;
        }
        // one team is not in addition to all of them
        onChange(all ? [team] : [...values, team]);
    };

    return (
        <div className="team-picker">
            <span className="setting-name">{label}</span>
            <div className="chips">
                {all
                    ? <span className="chip">All teams</span>
                    : values.map((v) =>
                        <span className="chip" key={v} title={name(v) || v}>
                            {v}
                            <button onClick={() => onChange(values.filter((x) => x !== v))} aria-label={`Remove ${v} from ${label.toLowerCase()}`}>&times;</button>
                        </span>)}
                {!all && values.length === 0 ? <span className="chip-empty">None</span> : null}
            </div>
            <div className="team-add">
                {teams.length > 0
                    ? <select value="" onChange={(e) => add(e.target.value)} aria-label={`Add to ${label.toLowerCase()}`}>
                        <option value="">Add a team&hellip;</option>
                        {teams.filter((t) => !values.includes(t.abbreviation)).map((t) =>
                            <option key={t.abbreviation} value={t.abbreviation}>{t.name} ({t.abbreviation})</option>)}
                    </select>
                    : null}
                {allowAll
                    ? <>
                        <input
                            value={other}
                            onChange={(e) => setOther(e.target.value)}
                            placeholder="Conference or TOP25"
                            aria-label={`Add a conference or ranking to ${label.toLowerCase()}`}
                        />
                        <button
                            className="time-add"
                            disabled={!other.trim()}
                            onClick={() => { add(other.trim().toUpperCase()); setOther(''); }}
                        >Add</button>
                        {!all ? <button className="time-add" onClick={() => onChange(['ALL'])}>All teams</button> : null}
                    </>
                    : null}
            </div>
        </div>
    );
}

// LocationSetting is where the weather boards report for. It is saved when
// Save is pressed, rather than as it is typed, since the Pi checks it with the
// weather provider first.
function LocationSetting({ board, location, place, onSave }) {
    const [draft, setDraft] = useState(location || '');
    const [busy, setBusy] = useState(false);

    useEffect(() => { setDraft(location || ''); }, [location]);

    const tidy = ParseLocation(draft);
    const unchanged = tidy !== null && tidy === ParseLocation(location);

    const save = async () => {
        setBusy(true);
        try {
            await onSave(tidy);
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="location-setting">
            <label className="setting-name" htmlFor={`location-${board.name}`}>Location</label>
            <div className="location-edit">
                <input
                    id={`location-${board.name}`}
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter' && tidy && !unchanged && !busy) save(); }}
                    placeholder="41.8858, -87.6181"
                    inputMode="decimal"
                    autoComplete="off"
                    spellCheck={false}
                />
                <button className="time-add" onClick={save} disabled={!tidy || unchanged || busy}>
                    {busy ? 'Checking\u2026' : 'Save'}
                </button>
            </div>
            <p className="setting-hint">
                {location
                    ? (place ? `Weather for ${place}. ` : '')
                    : 'Not set yet: the weather boards skip their turn until it is. '}
                In Google Maps, right-click a spot and click the numbers at the top of the menu to copy them.
            </p>
        </div>
    );
}

// ShowPicker is the TV shows a board follows. A show is added by searching
// TVmaze and picking it from what comes back, so that it is the show meant and
// not just one with a similar name.
function ShowPicker({ board, shows, onChange, onError }) {
    const [query, setQuery] = useState('');
    const [found, setFound] = useState(null);
    const [busy, setBusy] = useState(false);
    const following = new Set(shows.map((s) => s.id));

    const search = async () => {
        if (!query.trim()) {
            return;
        }
        setBusy(true);
        try {
            const resp = await CallRPC('matrix.v1.Sportsmatrix/SearchShows', { name: board.name, query: query.trim() });
            setFound(resp.shows || []);
            onError?.('');
        } catch (err) {
            onError?.(err.message);
        } finally {
            setBusy(false);
        }
    };

    const add = async (show) => {
        await onChange([...shows, show]);
        setFound(null);
        setQuery('');
    };

    const about = (s) => [s.network, s.premiered].filter(Boolean).join(', ');

    return (
        <div className="show-picker">
            <span className="setting-name">Shows</span>
            <div className="chips">
                {shows.map((s) =>
                    <span className="chip" key={s.id || s.name} title={about(s)}>
                        {s.name}
                        <button onClick={() => onChange(shows.filter((x) => x !== s))} aria-label={`Stop following ${s.name}`}>&times;</button>
                    </span>)}
                {shows.length === 0 ? <span className="chip-empty">None yet</span> : null}
            </div>
            <div className="show-search">
                <input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter') search(); }}
                    placeholder="Find a show"
                    aria-label="Find a show to follow"
                    autoComplete="off"
                />
                <button className="time-add" onClick={search} disabled={!query.trim() || busy}>
                    {busy ? 'Searching\u2026' : 'Search'}
                </button>
            </div>
            {found
                ? <ul className="show-results">
                    {found.length === 0 ? <li className="chip-empty">Nothing on TVmaze by that name</li> : null}
                    {found.map((s) =>
                        <li key={s.id}>
                            <span>
                                {s.name}
                                {about(s) ? <span className="show-about"> {about(s)}</span> : null}
                            </span>
                            {following.has(s.id)
                                ? <span className="chip-empty">Following</span>
                                : <button className="time-add" onClick={() => add(s)}>Add</button>}
                        </li>)}
                </ul>
                : null}
            <p className="setting-hint">Only new episodes are shown, never reruns.</p>
        </div>
    );
}

// BoardSettings are a board's display time, for a league the teams it shows
// and its favorites, for the weather its location, and for the TV board its
// shows. Each change is saved as it is made.
export default function BoardSettings({ board, onError }) {
    const [settings, setSettings] = useState(null);

    const load = useCallback(async () => {
        try {
            setSettings(await CallRPC('matrix.v1.Sportsmatrix/GetBoardSettings', { name: board.name }));
        } catch (err) {
            onError?.(err.message);
        }
    }, [board.name, onError]);

    useEffect(() => { load(); }, [load]);

    const save = async (change) => {
        try {
            await CallRPC('matrix.v1.Sportsmatrix/SetBoardSettings', { name: board.name, ...change });
            onError?.('');
        } catch (err) {
            onError?.(err.message);
        }
        await load();
    };

    if (!settings || (!settings.has_board_delay && !settings.has_teams && !settings.has_location && !settings.has_shows)) {
        return null;
    }

    const watch = settings.watch_teams || [];
    const favorite = settings.favorite_teams || [];
    const teams = settings.teams || [];

    return (
        <div className="board-own-settings">
            {settings.has_board_delay
                ? <div className="setting-row">
                    <label htmlFor={`delay-${board.name}`}>
                        {settings.has_teams ? 'Show each game for' : settings.has_shows ? 'Show each episode for' : 'Show for'}
                    </label>
                    <select
                        id={`delay-${board.name}`}
                        value={settings.board_delay}
                        onChange={(e) => save({ board_delay: e.target.value })}
                    >
                        {DelayChoices(settings.board_delay, settings.min_board_delay).map((d) =>
                            <option key={d} value={d}>{d}</option>)}
                    </select>
                </div>
                : null}
            {settings.has_teams
                ? <>
                    <TeamPicker
                        label="Teams shown"
                        values={watch}
                        teams={teams}
                        allowAll
                        onChange={(next) => save({ has_teams: true, watch_teams: next, favorite_teams: favorite })}
                    />
                    <TeamPicker
                        label="Favorites"
                        values={favorite}
                        teams={teams}
                        onChange={(next) => save({ has_teams: true, watch_teams: watch, favorite_teams: next })}
                    />
                </>
                : null}
            {settings.has_shows
                ? <ShowPicker
                    board={board}
                    shows={settings.shows || []}
                    onChange={(next) => save({ has_shows: true, shows: next.map((x) => ({ id: x.id, name: x.name })) })}
                    onError={onError}
                />
                : null}
            {settings.has_location
                ? <LocationSetting
                    board={board}
                    location={settings.location}
                    place={settings.location_place}
                    onSave={(location) => save({ has_location: true, location })}
                />
                : null}
        </div>
    );
}
