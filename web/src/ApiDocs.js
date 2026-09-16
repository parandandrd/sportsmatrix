import React from 'react';
import SwaggerUI from 'swagger-ui-react';
import "swagger-ui-react/swagger-ui.css";
import swag from './matrix.swagger.json';

// ApiDocs is its own chunk. Swagger UI is most of what the app weighs, and it
// used to be downloaded, parsed and run on every page load of the dashboard.
export default function ApiDocs() {
    return <SwaggerUI spec={swag} />;
}
