type Props = {
  colors: string[];
  selected: number;
  onSelect: (index: number) => void;
};

export function Palette({ colors, selected, onSelect }: Props) {
  return (
    <div className="palette" role="radiogroup" aria-label="Color">
      {colors.map((color, i) => (
        <button
          key={color}
          role="radio"
          aria-checked={i === selected}
          aria-label={color}
          className="swatch"
          style={{ background: color }}
          onClick={() => onSelect(i)}
        />
      ))}
    </div>
  );
}
