type Props = {
  colors: string[];
  names: string[];
  selected: number;
  onSelect: (index: number) => void;
};

export function Palette({ colors, names, selected, onSelect }: Props) {
  return (
    <div className="palette" role="radiogroup" aria-label="Color">
      {colors.map((color, i) => (
        <button
          key={color}
          role="radio"
          aria-checked={i === selected}
          aria-label={names[i] ?? color}
          title={names[i] ?? color}
          className="swatch"
          style={{ background: color }}
          onClick={() => onSelect(i)}
        />
      ))}
    </div>
  );
}
