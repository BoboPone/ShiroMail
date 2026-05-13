import { cn } from "@/lib/utils";

type SparklineProps = {
  data: number[];
  width?: number;
  height?: number;
  color?: string;
  className?: string;
};

export function Sparkline({
  data,
  width = 120,
  height = 32,
  color = "currentColor",
  className,
}: SparklineProps) {
  if (!data.length) return null;

  const max = Math.max(...data);
  const min = Math.min(...data);
  const range = max - min || 1;

  // Padding so the line doesn't clip at edges
  const padX = 2;
  const padY = 2;
  const innerW = width - padX * 2;
  const innerH = height - padY * 2;

  const points = data.map((value, i) => {
    const x = padX + (data.length > 1 ? (i / (data.length - 1)) * innerW : innerW / 2);
    const y = padY + innerH - ((value - min) / range) * innerH;
    return `${x},${y}`;
  });

  const polylinePoints = points.join(" ");

  // Closed polygon for the filled area beneath the line
  const areaPoints = [
    `${padX},${height - padY}`,
    ...points,
    `${width - padX},${height - padY}`,
  ].join(" ");

  return (
    <svg
      aria-hidden="true"
      className={cn("shrink-0", className)}
      fill="none"
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      width={width}
    >
      <polygon fill={color} opacity={0.12} points={areaPoints} />
      <polyline
        fill="none"
        points={polylinePoints}
        stroke={color}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={1.5}
      />
    </svg>
  );
}
