import React, { useEffect, useState } from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import ToggleButton from 'react-bootstrap/ToggleButton';
import ToggleButtonGroup from 'react-bootstrap/ToggleButtonGroup';
import { BrowserStorage, InitialView, SaveView, useFrames, usePageVisible } from './frames';
import './Board.css';

// Board is the live board, meant to be left up on a screen. It shows the
// panel's own frames, or the boards drawn again at full size; ?view=panel or
// ?view=full picks one from the URL, and the toggle remembers the choice.
export default function Board() {
    const [view, setView] = useState(() => InitialView(window.location.search, BrowserStorage()));
    const visible = usePageVisible();
    const frame = useFrames(view, visible);

    useEffect(() => {
        document.body.style.backgroundColor = 'black';
        return () => {
            document.body.style.backgroundColor = '';
        };
    }, []);

    const choose = (v) => {
        setView(v);
        SaveView(BrowserStorage(), v);
    };

    return (
        <div className="webboard">
            {frame.src
                ? <img
                    className={frame.from === 'panel' ? 'webboard-frame pixelated' : 'webboard-frame'}
                    src={frame.src}
                    alt="What the panel is showing"
                />
                : null}
            <div className="webboard-controls">
                <ToggleButtonGroup type="radio" name="view" size="sm" value={view} onChange={choose}>
                    <ToggleButton id="view-panel" value="panel" variant="outline-light">Panel</ToggleButton>
                    <ToggleButton id="view-full" value="full" variant="outline-light">Full-res</ToggleButton>
                </ToggleButtonGroup>
                {view === 'full' && frame.from === 'panel'
                    ? <div className="webboard-note">Full-res starts with the next board</div>
                    : null}
            </div>
        </div>
    );
}
