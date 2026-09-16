import React, { useCallback, useEffect, useState } from 'react';
import { CallRPC } from './util';
import { DelayChoices } from './delay';

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

// BoardSettings are a board's display time, and for a league, the teams it
// shows and its favorites. Each change is saved as it is made.
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

    if (!settings || (!settings.has_board_delay && !settings.has_teams)) {
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
                        {settings.has_teams ? 'Show each game for' : 'Show for'}
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
        </div>
    );
}
