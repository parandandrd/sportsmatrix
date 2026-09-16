import React from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Button from 'react-bootstrap/Button';
import Container from 'react-bootstrap/Container';
import Row from 'react-bootstrap/Row';
import Col from 'react-bootstrap/Col';
import Form from 'react-bootstrap/Form';
import Image from 'react-bootstrap/Image';
import { CallRPC, MatrixPostRet, JSONToStatus, JumpToBoard } from './util';
import { SetStatusReq, Status } from './sportboard/sportboard_pb';
import * as basicboard_pb from './basicboard/basicboard_pb';
import { LogoSrc } from './Logo';


function jsonToStatus(jsonDat) {
    var d = JSON.parse(jsonDat);
    var dat = d.status;
    var status = new Status();
    status.setEnabled(dat.enabled);
    status.setFavoriteHidden(dat.favorite_hidden);
    status.setFavoriteSticky(dat.favorite_sticky);
    status.setRecordRankEnabled(dat.record_rank_enabled);
    status.setOddsEnabled(dat.odds_enabled);
    status.setUseGradient(dat.use_gradient);
    status.setLiveOnly(dat.live_only);
    status.setDetailedLive(dat.detailed_live);
    status.setShowLeagueLogo(dat.show_league_logo);

    return status;
}

class Sport extends React.Component {
    constructor(props) {
        super(props);
        var status = new Status();
        this.state = {
            "status": status,
            "stats": new basicboard_pb.Status(),
            "headlines": new basicboard_pb.Status(),
            "has_stats": false,
        };
        if (this.props.sport === "nhl") {
            console.log("Sport created ", this.props.sport, this.state.status)
        }
    }
    async componentDidMount() {
        await this.getStatus()
        if (this.props.sport === "nhl") {
            console.log("Sport Updated " + this.props.sport + " " + this.state.enabled)
        }
    }
    getStatus = async () => {
        const fetchStatus = (path) => MatrixPostRet(path, '{}').then((resp) => {
            if (resp.ok) {
                return resp.text();
            }
            throw resp;
        });
        // stats and headlines are false when this league is known not to have
        // those boards, and undefined when nobody said
        const maybe = (present, path) => (present === false
            ? Promise.reject(new Error("no such board"))
            : fetchStatus(path));

        // Three services with nothing to wait on each other for. These went one
        // after another, a round trip to the Pi apiece.
        const [sport, stats, headlines] = await Promise.allSettled([
            fetchStatus(this.props.sport + "/sport.v1.Sport/GetStatus"),
            maybe(this.props.stats, "stat/" + this.props.sport + "/board.v1.BasicBoard/GetStatus"),
            maybe(this.props.headlines, "headlines/" + this.props.sport + "/board.v1.BasicBoard/GetStatus"),
        ]);

        const next = {};
        if (sport.status === "fulfilled") {
            next.status = jsonToStatus(sport.value);
        }
        try {
            next.stats = JSONToStatus(stats.value);
            next.has_stats = true;
        } catch (e) {
            next.has_stats = false;
        }
        try {
            next.headlines = JSONToStatus(headlines.value);
            next.has_headlines = true;
        } catch (e) {
            next.has_headlines = false;
        }
        this.setState(next);
    }

    updateStatus = async () => {
        try {
            var req = new SetStatusReq();
            req.setStatus(this.state.status);
            await CallRPC(this.props.sport + "/sport.v1.Sport/SetStatus", req.toObject());

            if (this.state.has_stats) {
                var sreq = new basicboard_pb.SetStatusReq();
                sreq.setStatus(this.state.stats);
                await CallRPC("stat/" + this.props.sport + "/board.v1.BasicBoard/SetStatus", sreq.toObject());
            }

            if (this.state.has_headlines) {
                var hreq = new basicboard_pb.SetStatusReq();
                hreq.setStatus(this.state.headlines);
                await CallRPC("headlines/" + this.props.sport + "/board.v1.BasicBoard/SetStatus", hreq.toObject());
            }
            this.props.onError?.('');
        } catch (err) {
            this.props.onError?.(err.message);
        }
        await this.getStatus();
        this.props.doSync?.();
    }

    doJump = async () => {
        // Jump wants the board's name, which for NCAA Basketball, Ligue 1 and
        // others is not the slug in its path
        await JumpToBoard(this.props.name || this.props.sport);
        console.log("Syncing from sport")
        this.props.doSync?.();
    }

    render() {
        var img = (
            <Row className="text-center">
                <Col>
                    <Image src={LogoSrc(this.props.sport)} style={{ height: '100px', width: 'auto' }} fluid />
                </Col>
            </Row>
        )
        return (
            <Container fluid>
                {this.props.withImg ? img : ""}
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "enabler"} label="Enable/Disable" checked={this.state.status.getEnabled()}
                            onChange={() => { this.state.status.setEnabled(!this.state.status.getEnabled()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "stats"} label="Stats" checked={this.state.stats.getEnabled()} disabled={!this.state.has_stats}
                            onChange={() => { this.state.stats.setEnabled(!this.state.stats.getEnabled()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "headlines"} label="News Headlines" checked={this.state.headlines.getEnabled()} disabled={!this.state.has_headlines}
                            onChange={() => { this.state.headlines.setEnabled(!this.state.headlines.getEnabled()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "favscore"} label="Hide Favorite Scores" checked={this.state.status.getFavoriteHidden()}
                            onChange={() => { this.state.status.setFavoriteHidden(!this.state.status.getFavoriteHidden()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "record"} label="Record + Rank" checked={this.state.status.getRecordRankEnabled()}
                            onChange={() => { this.state.status.setRecordRankEnabled(!this.state.status.getRecordRankEnabled()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "favstick"} label="Stick Favorite Live Games" checked={this.state.status.getFavoriteSticky()}
                            onChange={() => { this.state.status.setFavoriteSticky(!this.state.status.getFavoriteSticky()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "gradient"} label="Logo Gradient" checked={this.state.status.getUseGradient()}
                            onChange={() => { this.state.status.setUseGradient(!this.state.status.getUseGradient()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "liveonly"} label="Live Games Only" checked={this.state.status.getLiveOnly()}
                            onChange={() => { this.state.status.setLiveOnly(!this.state.status.getLiveOnly()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "detailedlive"} label="Detailed Live View" checked={this.state.status.getDetailedLive()}
                            onChange={() => { this.state.status.setDetailedLive(!this.state.status.getDetailedLive()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "leaguelogo"} label="Show League Logo" checked={this.state.status.getShowLeagueLogo()}
                            onChange={() => { this.state.status.setShowLeagueLogo(!this.state.status.getShowLeagueLogo()); this.updateStatus(); }} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Button variant="primary" onClick={() => { this.doJump(); }}>Jump</Button>
                    </Col>
                </Row>
            </Container>
        )
    }
}

export default Sport;