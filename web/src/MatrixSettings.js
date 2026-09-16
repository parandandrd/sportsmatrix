import React, { useCallback, useEffect, useRef, useState } from 'react';
import { CallRPC } from './util';
import { ParseCron, ToCron } from './schedule';

function Brightness({ value, onSaved }) {
    const [level, setLevel] = useState(value);
    const [problem, setProblem] = useState('');
    const timer = useRef(null);

    useEffect(() => { setLevel(value); }, [value]);
    useEffect(() => () => clearTimeout(timer.current), []);

    // The panel follows the slider as it moves, a moment after it stops, rather
    // than on every step of a drag.
    const change = (next) => {
        setLevel(next);
        clearTimeout(timer.current);
        timer.current = setTimeout(async () => {
            try {
                await CallRPC('matrix.v1.Sportsmatrix/SetBrightness', { brightness: next });
                setProblem('');
                onSaved();
            } catch (err) {
                setProblem(err.message);
            }
        }, 350);
    };

    return (
        <div className="setting">
            <label htmlFor="brightness">Brightness</label>
            <div className="setting-control">
                <input
                    id="brightness"
                    type="range"
                    min="1"
                    max="100"
                    value={level}
                    onChange={(e) => change(Number(e.target.value))}
                />
                <span className="setting-value">{level}</span>
            </div>
            {problem ? <p className="dash-msg error" role="alert">{problem}</p> : null}
        </div>
    );
}

function Times({ label, entries, onChange }) {
    const set = (i, entry) => onChange(entries.map((e, j) => (j === i ? entry : e)));
    const remove = (i) => onChange(entries.filter((_, j) => j !== i));

    return (
        <div className="times">
            <span className="times-label">{label}</span>
            <div className="times-list">
                {entries.map((entry, i) =>
                    <span className="time" key={i}>
                        {entry.time !== undefined
                            ? <input
                                type="time"
                                value={entry.time}
                                onChange={(e) => set(i, { time: e.target.value })}
                                aria-label={`${label} ${i + 1}`}
                                required
                            />
                            : <code title="Edit this one in sportsmatrix.conf">{entry.spec}</code>}
                        <button onClick={() => remove(i)} aria-label={`Remove ${label.toLowerCase()} ${i + 1}`}>&times;</button>
                    </span>)}
                <button className="time-add" onClick={() => onChange([...entries, { time: '07:00' }])}>Add</button>
            </div>
        </div>
    );
}

function Schedule({ schedule, onSaved }) {
    const parse = (specs) => (specs || []).map(ParseCron);
    const [on, setOn] = useState(() => parse(schedule.on_times));
    const [off, setOff] = useState(() => parse(schedule.off_times));
    const [problem, setProblem] = useState('');
    const [saving, setSaving] = useState(false);

    const original = JSON.stringify([schedule.on_times || [], schedule.off_times || []]);
    useEffect(() => {
        const [onTimes, offTimes] = JSON.parse(original);
        setOn(onTimes.map(ParseCron));
        setOff(offTimes.map(ParseCron));
    }, [original]);

    const current = JSON.stringify([on.map(ToCron), off.map(ToCron)]);
    const changed = current !== original;
    const complete = [...on, ...off].every((e) => e.time !== '');

    const save = async () => {
        setSaving(true);
        try {
            await CallRPC('matrix.v1.Sportsmatrix/SetScreenSchedule', {
                on_times: on.map(ToCron),
                off_times: off.map(ToCron),
            });
            setProblem('');
            onSaved();
        } catch (err) {
            setProblem(err.message);
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="setting">
            <span className="setting-name">Screen</span>
            <Times label="On at" entries={on} onChange={setOn} />
            <Times label="Off at" entries={off} onChange={setOff} />
            {changed
                ? <div className="setting-actions">
                    <button className="btn-sm-matrix" onClick={save} disabled={saving || !complete}>
                        {saving ? 'Saving...' : 'Save schedule'}
                    </button>
                </div>
                : null}
            {problem ? <p className="dash-msg error" role="alert">{problem}</p> : null}
        </div>
    );
}

// MatrixSettings are the settings for the whole panel. Each is saved to the
// config file as it changes, so it is still set after a restart.
export default function MatrixSettings() {
    const [settings, setSettings] = useState(null);
    const [problem, setProblem] = useState('');

    const load = useCallback(async () => {
        try {
            setSettings(await CallRPC('matrix.v1.Sportsmatrix/GetSettings', {}));
            setProblem('');
        } catch (err) {
            setProblem(err.message);
        }
    }, []);

    useEffect(() => { load(); }, [load]);

    return (
        <div className="section">
            <div className="section-head">
                <h2>Settings</h2>
                {settings
                    ? <span className="section-note-inline">
                        {settings.config_file ? `Saved to ${settings.config_file}` : 'Not saved: no config file'}
                    </span>
                    : null}
            </div>
            {problem ? <p className="dash-msg error" role="alert">{problem}</p> : null}
            {settings
                ? <div className="settings">
                    <Brightness value={settings.brightness} onSaved={load} />
                    <Schedule schedule={settings.screen_schedule || {}} onSaved={load} />
                </div>
                : null}
        </div>
    );
}
