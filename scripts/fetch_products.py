#!/usr/bin/env python3
"""
重新生成 config/files/products_<locale>.json

机型数据来自 Apple 各地区购买页中内嵌的
window.PRODUCT_SELECTION_BOOTSTRAP.productSelectionData，
此前需要手动开发者工具复制，这里自动完成。

用法:
    python3 scripts/fetch_products.py                  # 全部地区
    python3 scripts/fetch_products.py --locale zh_CN   # 指定地区
    python3 scripts/fetch_products.py --dry-run        # 只报告，不写文件

新一代机型发布后，通常只需更新下面的 SLUGS。
可用的购买页可以从 https://www.apple.com/<shortcode>/shop/buy-iphone 页面里找到。
"""

import argparse
import json
import pathlib
import sys
import time
import urllib.error
import urllib.request

# (locale, shortCode) —— 必须与 model/area.go 中的 Areas 保持一致
AREAS = [
    ("zh_CN", "cn"),
    ("zh_HK", "hk-zh"),
    ("zh_TW", "tw"),
    ("en_SG", "sg"),
    ("ja_JP", "jp"),
    ("en_AU", "au"),
    ("en_MY", "my"),
]

# 购买页，顺序决定型号在下拉框中的先后
SLUGS = [
    "iphone-17",
    "iphone-17e",
    "iphone-air",
    "iphone-18-pro",
    "iphone-duo",
]

UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)
KEY = "productSelectionData:"
OUT_DIR = pathlib.Path(__file__).resolve().parent.parent / "config" / "files"


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept-Language": "en,zh;q=0.9"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read().decode("utf-8", errors="replace")


def extract(html):
    """括号配平地取出 productSelectionData 对象，需正确跳过字符串内的转义引号"""
    i = html.find(KEY)
    if i < 0:
        return None
    j = html.index("{", i + len(KEY))

    depth, in_str, esc = 0, False, False
    for k in range(j, len(html)):
        c = html[k]
        if in_str:
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
            continue
        if c == '"':
            in_str = True
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return json.loads(html[j : k + 1])
    return None


def families(data):
    return sorted({p.get("familyType") for p in data.get("products", [])})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--locale", action="append", help="只处理指定地区，可重复")
    ap.add_argument("--dry-run", action="store_true", help="只报告，不写文件")
    ap.add_argument("--delay", type=float, default=1.0, help="请求间隔秒数")
    args = ap.parse_args()

    areas = [a for a in AREAS if not args.locale or a[0] in args.locale]
    failed = False

    for locale, short in areas:
        blocks, report = [], []

        for slug in SLUGS:
            url = f"https://www.apple.com/{short}/shop/buy-iphone/{slug}"
            try:
                html = fetch(url)
            except urllib.error.HTTPError as e:
                # 机型并非每个地区都有售，404 属正常情况
                report.append(f"{slug}: HTTP {e.code}，跳过")
                continue
            except Exception as e:
                report.append(f"{slug}: 请求失败 {e}")
                failed = True
                continue

            data = extract(html)
            if data is None:
                report.append(f"{slug}: 未找到 productSelectionData")
                failed = True
                continue

            n = len(data.get("products", []))
            if n == 0:
                report.append(f"{slug}: products 为空，跳过")
                continue

            blocks.append(data)
            report.append(f"{slug}: {n} 个 SKU  {'+'.join(families(data))}")
            time.sleep(args.delay)

        print(f"\n=== {locale} ({short}) ===")
        for line in report:
            print(f"  {line}")

        if not blocks:
            print("  !! 没有任何数据，保留原文件")
            failed = True
            continue

        total = sum(len(b.get("products", [])) for b in blocks)
        print(f"  合计 {len(blocks)} 组 / {total} 个 SKU")

        if not args.dry_run:
            out = OUT_DIR / f"products_{locale}.json"
            out.write_text(json.dumps(blocks, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            print(f"  已写入 {out.relative_to(OUT_DIR.parent.parent)}")

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
