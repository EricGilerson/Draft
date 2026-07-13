import {useEffect, useState} from 'react';
import {hasSandboxHoursDecimal, parseSandboxHours, sandboxHoursDecimalWarning} from '../lib/sandboxHours';
import './SandboxHoursInput.css';

type Props = {
    label: string;
    value: number;
    min?: number;
    onChange: (value: number) => void;
    variant?: 'field' | 'inline';
};

export default function SandboxHoursInput({label, value, min = 0, onChange, variant = 'inline'}: Props) {
    const [raw, setRaw] = useState(String(value));

    useEffect(() => {
        setRaw(String(value));
    }, [value]);

    const warning = sandboxHoursDecimalWarning(raw, min);

    const input = (
        <>
            <input
                className="input"
                type="number"
                min={min}
                step="1"
                value={raw}
                onChange={(e) => {
                    const next = e.target.value;
                    setRaw(next);
                    if (!hasSandboxHoursDecimal(next) && next !== '' && next !== '-') {
                        onChange(parseSandboxHours(next, min));
                    }
                }}
                onBlur={() => {
                    const parsed = parseSandboxHours(raw, min);
                    setRaw(String(parsed));
                    onChange(parsed);
                }}
            />
            {warning && <span className="sandbox-hours-decimal-warning">{warning}</span>}
        </>
    );

    if (variant === 'field') {
        return (
            <div className="form-field">
                <label className="form-label">{label}</label>
                {input}
            </div>
        );
    }

    return (
        <label>
            {label}
            {input}
        </label>
    );
}
