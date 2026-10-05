import React from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Button from 'react-bootstrap/Button';
import Container from 'react-bootstrap/Container';
import Row from 'react-bootstrap/Row';
import Col from 'react-bootstrap/Col';
import Image from 'react-bootstrap/Image';
import Form from 'react-bootstrap/Form';
import { CallRPC, FetchStatus, JumpToBoard, Toggled } from './util';
import { LogoSrc } from './Logo';

class BasicBoard extends React.Component {
    constructor(props) {
        super(props);
        var path = this.props.name;
        if (this.props.path) {
            path = this.props.path;
        }
        this.state = {
            "path": path,
            "status": {},
        };
    }
    async componentDidMount() {
        await this.getStatus();
    }
    getStatus = async () => {
        this.setState({ "status": await FetchStatus(this.state.path + "/board.v1.BasicBoard/GetStatus") });
    }

    // updateStatus shows next at once and sends it, then reads back what the
    // board really did with it.
    updateStatus = async (next) => {
        this.setState(next);
        try {
            await CallRPC(this.state.path + "/board.v1.BasicBoard/SetStatus", { "status": next.status });
            this.props.onError?.('');
        } catch (err) {
            this.props.onError?.(err.message);
        }
        await this.getStatus();
        this.props.doSync?.();
    }

    doJump = async () => {
        await JumpToBoard(this.props.name);
        this.props.doSync?.();
    }

    render() {
        var img = (
            <Row className="text-center"><Col><Image src={LogoSrc(this.props.name)} style={{ height: '100px', width: 'auto' }} fluid /></Col></Row>
        )
        return (
            <Container fluid>
                {this.props.withImg ? img : ""}
                <Row className="text-left">
                    <Col>
                        <Form.Switch id={this.props.name + "enabler"} label="Enable/Disable" checked={Boolean(this.state.status.enabled)}
                            onChange={() => this.updateStatus({ status: Toggled(this.state.status, 'enabled') })} />
                    </Col>
                </Row>
                <Row className="text-left">
                    <Col>
                        <Button variant="primary" onClick={() => { this.doJump(); }}>Jump</Button>

                    </Col>
                </Row>
            </Container >
        )
    }
}

export default BasicBoard;