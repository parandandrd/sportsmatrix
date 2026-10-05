import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import Sport from './Sport';

afterEach(() => {
    delete globalThis.fetch;
});

// fakeBoards answers GetStatus from statuses, keyed by path, and records every
// SetStatus body.
function fakeBoards(statuses) {
    const sets = [];
    globalThis.fetch = vi.fn(async (url, opts) => {
        const path = url.replace(/^http:\/\/[^/]*\//, '');
        let body = '{}';
        if (path.endsWith('/GetStatus')) {
            const status = statuses[path.replace(/\/GetStatus$/, '')];
            if (!status) {
                return { status: 404, ok: false, statusText: 'Not Found', text: async () => '{"msg":"not found"}' };
            }
            body = JSON.stringify({ status: status });
        } else if (path.endsWith('/SetStatus')) {
            sets.push({ path: path, body: JSON.parse(opts.body) });
        }
        return { status: 200, ok: true, statusText: 'OK', text: async () => body };
    });
    return sets;
}

test('a switch sends the whole status back with that one field flipped', async () => {
    const sets = fakeBoards({
        'nhl/sport.v1.Sport': { enabled: true, favorite_hidden: false, live_only: true },
        'headlines/nhl/board.v1.BasicBoard': { enabled: false },
    });
    render(<Sport sport="nhl" name="NHL" />);

    const hide = screen.getByLabelText('Hide Favorite Scores');
    await waitFor(() => expect(screen.getByLabelText('Enable/Disable')).toBeChecked());
    expect(screen.getByLabelText('Live Games Only')).toBeChecked();
    expect(hide).not.toBeChecked();
    expect(screen.getByLabelText('News Headlines')).toBeEnabled();

    await userEvent.click(hide);
    await waitFor(() => expect(sets).toHaveLength(2));
    expect(sets[0]).toEqual({
        path: 'nhl/sport.v1.Sport/SetStatus',
        body: { status: { enabled: true, favorite_hidden: true, live_only: true } },
    });
    expect(sets[1]).toEqual({
        path: 'headlines/nhl/board.v1.BasicBoard/SetStatus',
        body: { status: { enabled: false } },
    });
});

test('a league without headlines greys that switch out and never sets it', async () => {
    const sets = fakeBoards({
        'mls/sport.v1.Sport': { enabled: false },
    });
    render(<Sport sport="mls" name="MLS" />);

    await waitFor(() => expect(screen.getByLabelText('News Headlines')).toBeDisabled());

    await userEvent.click(screen.getByLabelText('Enable/Disable'));
    await waitFor(() => expect(sets).toHaveLength(1));
    expect(sets[0]).toEqual({ path: 'mls/sport.v1.Sport/SetStatus', body: { status: { enabled: true } } });
});
