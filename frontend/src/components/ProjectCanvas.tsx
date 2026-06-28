import {Background, BackgroundVariant, ControlButton, Controls, ReactFlow, useReactFlow} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {Maximize2, Minus, Plus} from 'lucide-react';
import {store} from '../../wailsjs/go/models';
import './ProjectCanvas.css';

type ProjectCanvasProps = {
    project: store.Project;
};

function CanvasControls() {
    const {zoomIn, zoomOut, fitView} = useReactFlow();

    return (
        <Controls
            position="bottom-left"
            showZoom={false}
            showFitView={false}
            showInteractive={false}
            className="canvas-controls"
        >
            <ControlButton className="canvas-control-button" onClick={() => zoomIn()}>
                <Plus size={15}/>
            </ControlButton>
            <ControlButton className="canvas-control-button" onClick={() => zoomOut()}>
                <Minus size={15}/>
            </ControlButton>
            <ControlButton className="canvas-control-button" onClick={() => fitView({padding: 0.2})}>
                <Maximize2 size={15}/>
            </ControlButton>
        </Controls>
    );
}

export default function ProjectCanvas({project}: ProjectCanvasProps) {
    return (
        <div className="project-canvas" aria-label={`${project.name} canvas`}>
            <ReactFlow
                colorMode="dark"
                nodes={[]}
                edges={[]}
                fitView
                minZoom={0.4}
                maxZoom={1.6}
                proOptions={{hideAttribution: true}}
            >
                <Background variant={BackgroundVariant.Dots} gap={22} size={1}/>
                <CanvasControls/>
            </ReactFlow>
        </div>
    );
}
