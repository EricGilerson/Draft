import {ReactFlow, Background, BackgroundVariant, Controls} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {ArrowLeft} from 'lucide-react';
import {store} from '../../wailsjs/go/models';
import './ProjectCanvas.css';

type ProjectCanvasProps = {
    project: store.Project;
    onBack: () => void;
};

export default function ProjectCanvas({project, onBack}: ProjectCanvasProps) {
    return (
        <div className="project-canvas">
            <div className="canvas-header">
                <button className="btn btn-ghost canvas-back" onClick={onBack}>
                    <ArrowLeft size={16}/> Projects
                </button>
                <span className="canvas-project-name">{project.name}</span>
            </div>
            <div className="canvas-flow">
                <ReactFlow colorMode="dark" nodes={[]} edges={[]} fitView>
                    <Background variant={BackgroundVariant.Dots} gap={22} size={1}/>
                    <Controls showInteractive={false}/>
                </ReactFlow>
            </div>
        </div>
    );
}
