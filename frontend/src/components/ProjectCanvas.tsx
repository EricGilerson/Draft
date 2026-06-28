import {
    Background,
    BackgroundVariant,
    ControlButton,
    Controls,
    type Node,
    type Edge,
    ReactFlow,
    useReactFlow,
    useNodesState,
    useEdgesState,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {Maximize2, Minus, Plus, PlusCircle, X} from 'lucide-react';
import {useCallback, useMemo, useRef, useState} from 'react';
import {store} from '../../wailsjs/go/models';
import ServiceNode from './ServiceNode';
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

let nodeCounter = 0;

function generateId(): string {
    nodeCounter++;
    const hex = nodeCounter.toString(16).padStart(4, '0');
    const rand = Math.random().toString(16).slice(2, 10);
    return `svc-${hex}-${rand}`;
}

export default function ProjectCanvas({project}: ProjectCanvasProps) {
    const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
    const [edges, _setEdges, onEdgesChange] = useEdgesState<Edge>([]);
    const [showAddPopover, setShowAddPopover] = useState(false);
    const [newNodeName, setNewNodeName] = useState('');
    const inputRef = useRef<HTMLInputElement>(null);

    const nodeTypes = useMemo(() => ({service: ServiceNode}), []);

    const addNode = useCallback(() => {
        const id = generateId();
        const label = newNodeName.trim() || id;
        const x = 120 + Math.random() * 300;
        const y = 140 + Math.random() * 200;

        const node: Node = {
            id,
            type: 'service',
            position: {x, y},
            data: {label, status: 'stopped'},
        };

        setNodes((prev) => [...prev, node]);
        setNewNodeName('');
        setShowAddPopover(false);
    }, [newNodeName, setNodes]);

    const openPopover = () => {
        setShowAddPopover(true);
        setNewNodeName('');
        setTimeout(() => inputRef.current?.focus(), 0);
    };

    return (
        <div className="project-canvas" aria-label={`${project.name} canvas`}>
            <div className="canvas-workspace-label" aria-hidden="true">
                <span className="canvas-kicker">Local topology</span>
                <strong>{project.name}</strong>
                <span className="canvas-path">{project.path}</span>
            </div>

            <div className="canvas-add-wrapper">
                <button className="btn btn-primary canvas-add-btn" onClick={openPopover}>
                    <PlusCircle size={15}/> Add Service
                </button>
                {showAddPopover && (
                    <div className="canvas-add-popover">
                        <div className="canvas-add-popover-header">
                            <span>New service</span>
                            <button className="dialog-close" onClick={() => setShowAddPopover(false)}>
                                <X size={14}/>
                            </button>
                        </div>
                        <div className="canvas-add-popover-body">
                            <input
                                ref={inputRef}
                                className="input"
                                value={newNodeName}
                                onChange={(e) => setNewNodeName(e.target.value)}
                                placeholder="Service name (optional)"
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') addNode();
                                    if (e.key === 'Escape') setShowAddPopover(false);
                                }}
                            />
                            <button className="btn btn-primary" onClick={addNode}>
                                Add
                            </button>
                        </div>
                    </div>
                )}
            </div>

            <div className="canvas-lane-label canvas-lane-label-services" aria-hidden="true">services</div>
            <div className="canvas-lane-label canvas-lane-label-ports" aria-hidden="true">ports</div>
            <div className="canvas-lane-label canvas-lane-label-env" aria-hidden="true">env sync</div>
            <ReactFlow
                colorMode="dark"
                nodes={nodes}
                edges={edges}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                nodeTypes={nodeTypes}
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
