import React from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Button from 'react-bootstrap/Button';
import Container from 'react-bootstrap/Container';
import Row from 'react-bootstrap/Row';
import Col from 'react-bootstrap/Col';
import Form from 'react-bootstrap/Form';
import Image from 'react-bootstrap/Image';
import { CallRPC, FetchStatus, JumpToBoard, Toggled } from './util';
import { LogoSrc } from './Logo';


class Sport extends React.Component {
    constructor(props) {
        super(props);
        this.state = {
            "status": {},
            "headlines": {},
        };
    }
    async componentDidMount() {
        await this.getStatus()
    }
    getStatus = async () => {
        // headlines is false when this league is known not to have that board,
        // and undefined when nobody said
        const maybe = (present, path) => (present === false
            ? Promise.reject(new Error("no such board"))
            : FetchStatus(path));

        // Two services with nothing to wait on each other for, fetched together
        // rather than a round trip to the Pi apiece.
        const [sport, headlines] = await Promise.allSettled([
            FetchStatus(this.props.sport + "/sport.v1.Sport/GetStatus"),
            maybe(this.props.headlines, "headlines/" + this.props.sport + "/board.v1.BasicBoard/GetStatus"),
        ]);

        const next = { "has_headlines": headlines.status === "fulfilled" };
        if (sport.status === "fulfilled") {
            next.status = sport.value;
        }
        if (headlines.status === "fulfilled") {
            next.headlines = headlines.value;
        }
        this.setState(next);
    }

    // updateStatus shows next -- a new status, a new headlines status or both --
    // at once and sends both, then reads back what the boards really did.
    updateStatus = async (next) => {
        const state = { ...this.state, ...next };
        this.setState(next);
        try {
            await CallRPC(this.props.sport + "/sport.v1.Sport/SetStatus", { "status": state.status });

            if (state.has_headlines) {
                await CallRPC("headlines/" + this.props.sport + "/board.v1.BasicBoard/SetStatus", { "status": state.headlines });
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
                        <Form.Switch id={this.props.sport + "enabler"} label="Enable/Disable" checked={Boolean(this.state.status.enabled)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'enabled') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "headlines"} label="News Headlines" checked={Boolean(this.state.headlines.enabled)} disabled={!this.state.has_headlines}
                            onChange={() => this.updateStatus({ headlines: Toggled(this.state.headlines, 'enabled') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "favscore"} label="Hide Favorite Scores" checked={Boolean(this.state.status.favorite_hidden)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'favorite_hidden') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "record"} label="Record + Rank" checked={Boolean(this.state.status.record_rank_enabled)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'record_rank_enabled') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "favstick"} label="Stick Favorite Live Games" checked={Boolean(this.state.status.favorite_sticky)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'favorite_sticky') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "gradient"} label="Logo Gradient" checked={Boolean(this.state.status.use_gradient)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'use_gradient') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "liveonly"} label="Live Games Only" checked={Boolean(this.state.status.live_only)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'live_only') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "detailedlive"} label="Detailed Live View" checked={Boolean(this.state.status.detailed_live)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'detailed_live') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.sport + "leaguelogo"} label="Show League Logo" checked={Boolean(this.state.status.show_league_logo)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'show_league_logo') })} />
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