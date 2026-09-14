import { useRef } from "react";
import type { SelectionConfig } from "../types";
export const defaultSelection = (): SelectionConfig => ({
  enabled: true,
  buttons: [
    {
      id: "translate",
      name: "翻译",
      template: "请将以下内容翻译为{{language}}，只输出译文：\n{{text}}",
    },
    {
      id: "explain",
      name: "解释",
      template: "请用{{language}}解释以下内容：\n{{text}}",
    },
  ],
});
const variables = ["text", "language", "date", "time", "timezone"];
export function SelectionSettings({
  value,
  onChange,
  permission,
  requestPermission,
}: {
  value: SelectionConfig;
  onChange: (v: SelectionConfig) => void;
  permission?: boolean;
  requestPermission: () => void;
}) {
  const focused = useRef<HTMLTextAreaElement | null>(null);
  const activeID = useRef("");
  const update = (
    id: string,
    patch: Partial<SelectionConfig["buttons"][number]>,
  ) =>
    onChange({
      ...value,
      buttons: value.buttons.map((b) => (b.id === id ? { ...b, ...patch } : b)),
    });
  const unknown = [
    ...new Set(
      value.buttons
        .flatMap((b) =>
          Array.from(b.template.matchAll(/\{\{([^{}]+)\}\}/g), (m) => m[1]),
        )
        .filter((v) => !variables.includes(v)),
    ),
  ];
  return (
    <section className="selection-settings" aria-label="划词工具条设置">
      <label className="selection-toggle">
        <input
          type="checkbox"
          checked={value.enabled}
          onChange={(e) => onChange({ ...value, enabled: e.target.checked })}
        />
        启用划词工具条
      </label>
      <p className="muted">
        选中文字后自动显示，每次点击新建会话。已有草稿会保留。
      </p>
      <p>
        {permission ? "辅助功能权限已开启" : "需要辅助功能权限"}{" "}
        <button type="button" onClick={requestPermission}>
          {permission ? "检查权限" : "开启辅助功能权限"}
        </button>
      </p>
      {value.buttons.map((button, index) => (
        <fieldset key={button.id}>
          <label>
            按钮名称
            <input
              value={button.name}
              maxLength={30}
              onChange={(e) => update(button.id, { name: e.target.value })}
            />
          </label>
          <label>
            提示词模板
            <textarea
              value={button.template}
              rows={3}
              onFocus={(e) => {
                focused.current = e.currentTarget;
                activeID.current = button.id;
              }}
              onChange={(e) => update(button.id, { template: e.target.value })}
            />
          </label>
          <div className="selection-button-actions">
            <button
              type="button"
              disabled={index === 0}
              onClick={() => {
                const buttons = [...value.buttons];
                [buttons[index - 1], buttons[index]] = [
                  buttons[index],
                  buttons[index - 1],
                ];
                onChange({ ...value, buttons });
              }}
            >
              上移
            </button>
            <button
              type="button"
              onClick={() =>
                onChange({
                  ...value,
                  buttons: value.buttons.filter((b) => b.id !== button.id),
                })
              }
            >
              删除
            </button>
          </div>
        </fieldset>
      ))}
      <div className="template-variables">
        插入变量：
        {variables.map((v) => (
          <button
            type="button"
            key={v}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              const el = focused.current;
              const b = value.buttons.find((b) => b.id === activeID.current);
              if (!el || !b) return;
              const start = el.selectionStart,
                end = el.selectionEnd,
                token = `{{${v}}}`;
              update(b.id, {
                template:
                  b.template.slice(0, start) + token + b.template.slice(end),
              });
              requestAnimationFrame(() => {
                el.focus();
                el.setSelectionRange(
                  start + token.length,
                  start + token.length,
                );
              });
            }}
          >{`{{${v}}}`}</button>
        ))}
      </div>
      <small>
        text：选中文字；language：系统语言；date / time /
        timezone：点击时的日期、24 小时时间与时区。未使用 text
        不会附加选中文字。
      </small>
      {unknown.length > 0 && (
        <p role="status">
          未知变量 {unknown.join("、")} 将原样保留，仍可保存。
        </p>
      )}
      {!value.buttons.length && <p>没有按钮时不显示工具条。</p>}
      <div>
        <button
          type="button"
          disabled={value.buttons.length >= 20}
          onClick={() =>
            onChange({
              ...value,
              buttons: [
                ...value.buttons,
                {
                  id: crypto.randomUUID(),
                  name: "新按钮",
                  template: "{{text}}",
                },
              ],
            })
          }
        >
          添加按钮
        </button>
        <button
          type="button"
          onClick={() =>
            onChange({ ...value, buttons: defaultSelection().buttons })
          }
        >
          恢复默认按钮
        </button>
      </div>
    </section>
  );
}
