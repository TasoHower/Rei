package provider

import "testing"

func TestPresetsAreUsable(t *testing.T) {
	all := Presets()
	if len(all) < 5 {
		t.Fatalf("预设数量 = %d", len(all))
	}
	for _, p := range all {
		if p.ID == "" || p.BaseURL == "" || p.Protocol == "" {
			t.Fatalf("预设字段缺失：%+v", p)
		}
	}
	if len(Presets()) != len(all) {
		t.Fatal("Presets 应返回副本")
	}
}

func TestModelLookupAndPricing(t *testing.T) {
	meta, ok := Model("deepseek-chat")
	if !ok || meta.ContextWindow != 64_000 || meta.MaxOutput != 8_192 {
		t.Fatalf("模型元数据 = %+v, ok=%v", meta, ok)
	}
	price, ok := PricingFor("deepseek-chat")
	if !ok || price.Input != 270_000 {
		t.Fatalf("价表 = %+v, ok=%v", price, ok)
	}
	if _, ok := Model("不存在"); ok {
		t.Fatal("未知模型不应命中")
	}
}

func TestPresetFor(t *testing.T) {
	if p, ok := PresetFor("anthropic"); !ok || p.Protocol != ProtocolAnthropic {
		t.Fatalf("anthropic 预设 = %+v, ok=%v", p, ok)
	}
	if _, ok := PresetFor("nope"); ok {
		t.Fatal("未知厂商不应命中")
	}
}
