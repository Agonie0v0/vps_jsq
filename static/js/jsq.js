(function () {
  "use strict";
  function h(e) {
    return new Promise(t => setTimeout(t, e))
  }

  function f(e, t) {
    var a = Math.round(e * Math.pow(10, t)) / Math.pow(10, t),
      n = a.toString(),
      d = n.indexOf(".");
      d < 0 && (n += ".");
    for (var l = n.length - n.indexOf("."); l <= t; l++) n += "0";
    return n
  }

  function v(e) {
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(e); {
      let t = document.createElement("textarea");
      return t.value = e, t.style.position = "absolute", t.style.opacity = 0, t.style.left = "-999999px", t.style.top = "-999999px", document.body.appendChild(t), t.select(), new Promise((a, n) => {
          document.execCommand("copy") ? a() : n(), t.remove()
        })
    }
  }

  function c(e, t = "tips") {
    const a = document.getElementById("toast"),
      n = document.createElement("div");
      n.className = `toast ${t}`, n.style.marginBottom = "5px", n.innerHTML = e, a.appendChild(n), setTimeout(() => {
      a.removeChild(n)
    }, 6e3)
  }

  function u(e) {
    e == "show" ? (document.getElementById("calculate_text").style.display = "none", document.getElementById("calculate_loading").style.display = "block") : (document.getElementById("calculate_text").style.display ="block", document.getElementById("calculate_loading").style.display = "none")
  }

  function B() {
    window.location.replace(location.href)
  }

  function w() {
    u("show");
    const e = document.getElementById("exchange_rate"),
      t = document.getElementById("renew_money"),
      a = document.getElementById("expiry_date"),
      n = document.getElementById("trade_date"),
      deal = document.getElementById("transaction_amount"),
      d = document.getElementById("reference_rate").value,
      l = e.value,
      i = t.value,
      _ = document.getElementById("currency_code").value,
      g = document.getElementById("payment_cycle").value,
      y = a.value,
      p = n.value;
    if (e.classList.remove("error"), t.classList.remove("error"), a.classList.remove("error"), n.classList.remove("error"), l.trim() === "") return u("hide"), document.getElementById("exchange_rate").classList.add("error"), c("外币汇率不能为空", "error"), !1;
    if (i.trim() === "") return u("hide"), document.getElementById("renew_money").classList.add("error"), c("续费金额不能为空", "error"), !1;
    if (y.trim() === "") return u("hide"), document.getElementById("expiry_date").classList.add("error"), c("请选择到期时间", "error"), !1;
    if (p.trim() === "") return u("hide"), document.getElementById("trade_date").classList.add("error"), c("请选择交易日期", "error"), !1;
    const T = new Date(y);
    if (new Date(p) > T) return u("hide"), document.getElementById("trade_date").classList.add("error"), c("交易日期不能在到期时间之后", "error"), !1;
    const b = {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        exchange_rate: d,
        custom_exchange_rate: l,
        renew_money: i,
        transaction_amount: deal.value,
        currency_code: _,
        cycle: g,
        expiry_date: y,
        trade_date: p
      })
    };
    fetch(URL_JSQ, b).then(r => {
      if (!r.ok) {
        u("hide");
        const s = r.headers.get("Content-Type");
        return s && s.includes("application/json") ? r.json().then(m => {
          throw new Error(JSON.stringify(m))
        }) : r.text().then(m => {
          throw new Error(`错误状态: ${r.status}, 响应内容: ${m}`)
        })
      }
      return r.json()
    }).then(r => {
      if (r && r.status === "success") h(5).then(() => {
        document.querySelectorAll(".output_trade_date").forEach(o => {
          o.innerText = r.data.trade_date
        }), document.querySelectorAll(".output_exchange_rate").forEach(o => {
          o.innerText = r.data.exchange_rate
        }), document.querySelectorAll(".output_renewal").forEach(o => {
          o.innerText = r.data.renewal
        }), document.querySelectorAll(".output_remain_days").forEach(o => {
          o.innerText = r.data.remain_days + " 天"
        }), document.querySelectorAll(".output_expiry_date").forEach(o => {
          o.innerText = "(于 " + r.data.expiry_date + " 过期)"
        }), document.querySelectorAll(".output_remain_value").forEach(o => {
          o.innerText = r.data.remain_value + " 元"
        }), document.querySelectorAll(".output_total_value").forEach(o => {
          o.innerText = "(总 " + r.data.total_value + " 元)"
        }), document.querySelectorAll(".output_custom_future_value").forEach(o => {
          o.innerText = r.data.custom_remain_value + " 元"
        }), document.querySelectorAll(".output_custom_exchange_rate").forEach(o => {
          o.innerText = "(汇率 " + r.data.custom_exchange_rate + ")"
        });
        const hasDeal = r.data.transaction_amount !== "";
        document.getElementById("tr_transaction_show").style.display = hasDeal ? "" : "none";
        document.getElementById("tr_premium_show").style.display = hasDeal ? "" : "none";
        if (hasDeal) {
          document.querySelectorAll(".output_transaction_amount").forEach(o => {
            o.innerText = r.data.transaction_amount + " 元"
          });
          document.querySelectorAll(".output_premium").forEach(o => {
            o.innerText = (Number(r.data.premium) > 0 ? "+" : "") + r.data.premium + " 元"
          });
          document.querySelectorAll(".output_premium").forEach(o => {
            o.classList.toggle("premium-positive", Number(r.data.premium) > 0);
            o.classList.toggle("premium-negative", Number(r.data.premium) < 0)
          });
          document.querySelectorAll(".output_premium_rate").forEach(o => {
            o.innerText = "(" + (Number(r.data.premium) > 0 ? "溢价" : Number(r.data.premium) < 0 ? "折价" : "平价") + ")"
          });
        }
        let s = r.data.exchange_rate;
        s = typeof s > "u" ? "0.000" : s;
        let m = r.data.custom_exchange_rate;
        m = typeof m > "u" ? "0.000" : m, m !== "0.000" && s !== m ? document.getElementById("tr_custom_exchange_show").style.display = "" : document.getElementById("tr_custom_exchange_show").style.display = "none", u("hide"), document.getElementById("is_calculated").value = "1", document.getElementById("share_url").value =
        r.data.share_pic, document.getElementById("result").scrollIntoView({behavior: "smooth"})
      });
      else {
        u("hide");
        const s = r.message ? r.message : "接口数据格式错误";
        c(s, "error")
      }
    }).catch(r => {
      u("hide");
      const s = r && typeof r == "object" && "message" in r ? r.message : "获取数据失败",
            o = JSON.parse(s).message || "获取数据失败";
      c(o, "error")
    })
  }
  async function D() {
    try {
      const t = await (await fetch(URL_RATES)).json();
      if (t && t.status === "success") {
        const a = t.data.rates;
        let n = new Array;
        for (let l = 0; l < a.length; ++l) {
          const i = a[l],
            _ = i.currency_code,
            g = i.rate;
            n[_] = g
        }
        let d = t.data.last_update_date;
        d = typeof d > "u" ? "0000/00/00" : d, document.getElementById("updated_date").innerText = d, I(
          document.getElementById("currency_code"), n), document.getElementById("currency_code").addEventListener(
            "change",
            function () {
              I(this, n)
            })
      } else {
        const a = t.message ? t.message : "接口数据格式错误";
        c(a, "error")
      }
    } catch (e) {
      c("接口获取数据失败 <br/>" + e, "error")
    }
  }

  function E() {
    try {
      let e = document.getElementById("share_url").value;
      e = "![image](" + e + ")", v(e), h(600).then(() => {
        document.getElementById("copy_text").style.display = "flex", document.getElementById("copied_text").style.display = "none", c("图片地址已复制到剪切板", "success")
      })
    } catch (e) {
      c("复制到剪切板失败<br>" + e, "error")
    }
  }

  function I(e, t) {
    const a = e.value,
      n = f(a === "CNY" ? 1 : t[a], 3);
    document.getElementById("reference_rate").value = n, document.getElementById("exchange_rate").value = n
  }

  function S() {
    const e = new Date,
      t = new Date(e);
    t.setDate(e.getDate() + 1);
    const a = e.getFullYear(),
      n = String(e.getMonth() + 1).padStart(2, "0"),
      d = String(e.getDate()).padStart(2, "0"),
      l = `${a}-${n}-${d}`,
      i = t.getFullYear(),
      _ = String(t.getMonth() + 1).padStart(2, "0"),
      g = String(t.getDate()).padStart(2, "0"),
      y = `${i}-${_}-${g}`;
    document.getElementById("trade_date").value = l, document.getElementById("expiry_date").value = y, document.getElementById("trade_date")._flatpickr && document.getElementById("trade_date")._flatpickr.setDate(l),document.getElementById("expiry_date")._flatpickr && document.getElementById("expiry_date")._flatpickr.setDate(y)
  }

  function L() {
    S(), flatpickr("#expiry_date", {
      dateFormat: "Y-m-d",
      minDate: "today",
      onChange: function (e, t) {
        document.getElementById("trade_date")._flatpickr
      }
    }), flatpickr("#trade_date", {
      dateFormat: "Y-m-d",
      minDate: "today"
    })
  }

  function x() {
    document.querySelectorAll(
      "#exchange_rate, #renew_money, #transaction_amount, #currency_code, #payment_cycle, #expiry_date, #trade_date").forEach(
        e => {
          e.addEventListener("change", function (t) {
            document.getElementById("is_calculated").value = "0"
          })
        }), document.querySelector("h1").addEventListener("click", e => {
          B()
        }), document.getElementById("view_btn").addEventListener("click", e => {
          const t = document.getElementById("is_calculated").value;
          if (t === "1") {
            if (document.getElementById("share_url").value !== "") {
              const n = document.getElementById("modal"),
                    d = document.getElementById("modal_img");
                    d.src = document.getElementById("share_url").value, n.style.display = "flex"
            }
          } else c(t === "" ? "请先计算剩余价值，再获取分享链接" : "数据已更改，请先计算剩余价值，再获取分享链接", "error")
        }), document.getElementById("copy_btn").addEventListener("click", e => {
          const t = document.getElementById("is_calculated").value;
          if (t == 0 || t == "") {
            const a = t == "" ? "请先计算剩余价值，再获取分享链接" : "数据已更改，请先计算剩余价值，再获取分享链接";
            c(a, "error")
          } else document.getElementById("share_url").value == "1" || document.getElementById("share_url").value !== "" ? (document.getElementById("copy_text").style.display = "none", document.getElementById("copied_text").style.display = "block", E()) : c("获取图片地址失败，请重新获取", "error")
        }), document.getElementById("exchange_rate").addEventListener("focus", () => {
          document.getElementById("exchange_rate").classList.remove("error")
        }), document.getElementById("renew_money").addEventListener("focus", () => {
          document.getElementById("renew_money").classList.remove("error")
        }), document.getElementById("trade_date").addEventListener("focus", () => {
          document.getElementById("trade_date").classList.remove("error")
        }), document.getElementById("expiry_date").addEventListener("focus", () => {
          document.getElementById("expiry_date").classList.remove("error")
        }), document.getElementById("frm").addEventListener("submit", e => (e.preventDefault(), w(), !1)),
        document.querySelector(".close").addEventListener("click", function () {
          modal.style.display = "none"
        }), window.addEventListener("click", function (e) {
          e.target === modal && (modal.style.display = "none")
        }), L(), D()
  }
  document.readyState !== "loading" ? x() : document.addEventListener("DOMContentLoaded", x, { once: !0 })
})();
