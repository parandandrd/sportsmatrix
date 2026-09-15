import { DescribeBoard } from './util';

// The dashboard picks a component and an RPC path purely from what ListBoards
// reports. Name cannot carry that: sport boards report the league's full name
// ("NCAA Basketball"), which is neither the route nor the RPC prefix.
test('DescribeBoard reads kind and path out of the rpc path', () => {
    expect(DescribeBoard({ name: 'NCAA Basketball', enabled: true, rpc_path: '/ncaam/sport.v1.Sport/' }))
        .toMatchObject({ name: 'NCAA Basketball', enabled: true, kind: 'sport', path: 'ncaam' });

    expect(DescribeBoard({ name: 'NHL Headlines', rpc_path: '/headlines/nhl/board.v1.BasicBoard/' }))
        .toMatchObject({ kind: 'basic', path: 'headlines/nhl' });

    expect(DescribeBoard({ name: 'StatBoard: NHL', rpc_path: '/stat/nhl/board.v1.BasicBoard/' }))
        .toMatchObject({ kind: 'basic', path: 'stat/nhl' });

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
