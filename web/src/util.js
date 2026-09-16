import * as basicboard_pb from './basicboard/basicboard_pb';
import * as sportsmatrix_pb from './sportsmatrix/sportsmatrix_pb';

export var BACKEND = "http://" + window.location.host

export function MatrixPostRet(path, body) {
    const req = {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: body,
    }
    //console.log(`Matrix POST ${BACKEND}/${path} ${body}`)
    return fetch(`${BACKEND}/${path}`, req)
}
export async function GetVersion(callback) {
    return await MatrixPostRet("matrix.v1.Sportsmatrix/Version", '{}').then((resp) => {
        if (resp.ok) {
            return resp.text()
        }
        throw resp
    }).then((data) => {
        var d = JSON.parse(data);
        callback(d.version);
    }).catch(err => {
        console.log("failed to get version", err);
    }
    );
}

export function JSONToStatus(jsonDat) {
    var d = JSON.parse(jsonDat);
    var dat = d.status;
    var status = new basicboard_pb.Status();
    status.setEnabled(dat.enabled);

    return status;
}

export async function JumpToBoard(board) {
    var req = new sportsmatrix_pb.JumpReq();
    req.setBoard(board);
    var r = JSON.stringify(req.toObject());
    console.log("Board Jump", "matrix.v1.Sportsmatrix/Jump", r);
    await MatrixPostRet("matrix.v1.Sportsmatrix/Jump", r);
}
// Board kinds, keyed by the twirp service a board mounts. The service name is
// the only reliable signal of what a board is: Name is a display name, and for
// sport boards it is the league's full name ("NCAA Basketball"), not its slug.
const boardKinds = {
    'sport.v1.Sport': 'sport',
    'racing.v1.Racing': 'racing',
    'board.v1.BasicBoard': 'basic',
    'imageboard.v1.ImageBoard': 'image',
};

// DescribeBoard turns one ListBoards entry into what the UI needs: which
// component drives it, and the path prefix that component posts to.
//
//   "/nhl/sport.v1.Sport/"               -> kind sport, path "nhl"
//   "/headlines/nhl/board.v1.BasicBoard/" -> kind basic, path "headlines/nhl"
//   "/imageboard.v1.ImageBoard/"          -> kind image, path ""
export function DescribeBoard(info) {
    const board = {
        name: info.name,
        enabled: Boolean(info.enabled),
        inBetween: Boolean(info.in_between),
        rpcPath: info.rpc_path || "",
        section: info.section || "",
        // a server that predates the field says nothing either way
        inConfigFile: info.in_config_file !== false,
        kind: "",
        path: "",
    };

    const parts = board.rpcPath.split('/').filter((p) => p !== "");
    if (parts.length > 0) {
        const kind = boardKinds[parts[parts.length - 1]];
        if (kind) {
            board.kind = kind;
            board.path = parts.slice(0, -1).join('/');
        }
    }

    return board;
}

// ListBoards reports the boards this instance was actually configured with.
export async function ListBoards() {
    const resp = await MatrixPostRet("matrix.v1.Sportsmatrix/ListBoards", '{}');
    if (!resp.ok) {
        throw new Error(`ListBoards failed: ${resp.status}`);
    }

    const data = await resp.json();
    return (data.boards || []).map(DescribeBoard);
}

// CallRPC posts to a Twirp method and returns its JSON answer. A failed call
// throws an Error carrying the server's own explanation -- "could not save that
// to /etc/sportsmatrix.conf", say -- rather than a bare status code.
export async function CallRPC(path, body) {
    const resp = await MatrixPostRet(path, typeof body === 'string' ? body : JSON.stringify(body || {}));
    const text = await resp.text();
    let data = null;
    try {
        data = text ? JSON.parse(text) : null;
    } catch (e) {
        // not every answer is JSON
    }
    if (!resp.ok) {
        throw new Error((data && data.msg) || `${resp.status} ${resp.statusText}`);
    }
    return data;
}

// SetBoardEnabled turns one board on or off by the name ListBoards reported.
export async function SetBoardEnabled(name, enabled) {
    await CallRPC("matrix.v1.Sportsmatrix/SetBoardEnabled", { name: name, enabled: Boolean(enabled) });
}
