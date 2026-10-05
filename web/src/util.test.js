import { DescribeBoard, FetchStatus, JumpToBoard, Toggled } from './util';

// The dashboard picks a component and an RPC path purely from what ListBoards
// reports. Name cannot carry that: sport boards report the league's full name
// ("NCAA Basketball"), which is neither the route nor the RPC prefix.
test('DescribeBoard reads kind and path out of the rpc path', () => {
    expect(DescribeBoard({ name: 'NCAA Basketball', enabled: true, rpc_path: '/ncaam/sport.v1.Sport/' }))
        .toMatchObject({ name: 'NCAA Basketball', enabled: true, kind: 'sport', path: 'ncaam' });

    expect(DescribeBoard({ name: 'NHL Headlines', rpc_path: '/headlines/nhl/board.v1.BasicBoard/' }))
        .toMatchObject({ kind: 'basic', path: 'headlines/nhl' });

    expect(DescribeBoard({ name: 'f1', rpc_path: '/f1/racing.v1.Racing/' }))
        .toMatchObject({ kind: 'racing', path: 'f1' });

    // the image board mounts at the root, so it has no prefix of its own
    expect(DescribeBoard({ name: 'Img', rpc_path: '/imageboard.v1.ImageBoard/' }))
        .toMatchObject({ kind: 'image', path: '' });
});

test('DescribeBoard leaves a board with no service alone', () => {
    expect(DescribeBoard({ name: 'Something', enabled: false, rpc_path: '' }))
        .toMatchObject({ name: 'Something', enabled: false, kind: '', path: '' });

    // a service this build does not know must not be guessed at
    expect(DescribeBoard({ name: 'Future', rpc_path: '/future/some.v1.Thing/' }))
        .toMatchObject({ kind: '', path: '' });
});

test('DescribeBoard normalises the flags it is given', () => {
    expect(DescribeBoard({ name: 'Clock', in_between: true }))
        .toMatchObject({ enabled: false, inBetween: true, rpcPath: '' });
});

afterEach(() => {
    delete globalThis.fetch;
});

function answer(status, body) {
    return {
        status,
        ok: status >= 200 && status < 300,
        statusText: '',
        text: async () => body,
    };
}

// A board's switches are its GetStatus answer as Twirp's JSON gives it, field
// names as in the .proto, sent back to SetStatus with one of them flipped.
test('FetchStatus returns the status object from a GetStatus answer', async () => {
    globalThis.fetch = vi.fn(async () => answer(200, '{"status":{"enabled":true,"favorite_hidden":false}}'));
    expect(await FetchStatus('nhl/sport.v1.Sport/GetStatus')).toEqual({ enabled: true, favorite_hidden: false });
    expect(globalThis.fetch.mock.calls[0][0]).toMatch(/\/nhl\/sport\.v1\.Sport\/GetStatus$/);
    expect(globalThis.fetch.mock.calls[0][1].body).toBe('{}');

    globalThis.fetch = vi.fn(async () => answer(200, '{}'));
    expect(await FetchStatus('img/imageboard.v1.ImageBoard/GetStatus')).toEqual({});

    globalThis.fetch = vi.fn(async () => answer(404, '{"msg":"no such board"}'));
    await expect(FetchStatus('x/board.v1.BasicBoard/GetStatus')).rejects.toThrow('no such board');
});

test('Toggled flips one field of a copy', () => {
    const status = { enabled: true, live_only: false };
    expect(Toggled(status, 'live_only')).toEqual({ enabled: true, live_only: true });
    expect(Toggled(status, 'enabled')).toEqual({ enabled: false, live_only: false });
    // a field the server left out is false
    expect(Toggled(status, 'use_gradient')).toEqual({ enabled: true, live_only: false, use_gradient: true });
    expect(status).toEqual({ enabled: true, live_only: false });
});

test('JumpToBoard posts the board name to Jump', async () => {
    globalThis.fetch = vi.fn(async () => answer(200, '{}'));
    await JumpToBoard('NCAA Basketball');
    expect(globalThis.fetch.mock.calls[0][0]).toMatch(/\/matrix\.v1\.Sportsmatrix\/Jump$/);
    expect(JSON.parse(globalThis.fetch.mock.calls[0][1].body)).toEqual({ board: 'NCAA Basketball' });
});
