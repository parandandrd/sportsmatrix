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
        };
    }
    async componentDidMount() {
        await this.getStatus()
    }
    getStatus = async () => {
        this.setState({ "status": await FetchStatus(this.props.sport + "/racing.v1.Racing/GetStatus") });
    }

    // updateStatus shows next at once and sends it, then reads back what the
    // board really did with it.
    updateStatus = async (next) => {
        this.setState(next);
        try {
            await CallRPC(this.props.sport + "/racing.v1.Racing/SetStatus", { "status": next.status });
            this.props.onError?.('');
        } catch (err) {
            this.props.onError?.(err.message);
        }
        await this.getStatus();
        this.props.doSync?.();
    }

    doJump = async () => {
        await JumpToBoard(this.props.name || this.props.sport);
        console.log("Syncing from racing")
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
                        <Button variant="primary" onClick={() => { this.doJump(); }}>Jump</Button>
                    </Col>
                </Row>
            </Container>
        )
    }
}

export default Sport;