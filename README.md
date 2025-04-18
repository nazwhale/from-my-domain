# From My Domain

Socialist email sending that liberates your domain. Cheap, clean. For Cloudflare domains + Gmail ✊

##  Next steps: 
| #  | Step                                                                                 | Why this order matters                                                                                                                                          |
|----|--------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **1** | **Spin up the VPS** (Hetzner, DO, Netcup…), request **port 25 unlock**               | You need a static, un‑blocked IP before you can publish SPF, rDNS, or DKIM keys for it.                                                                         |
| **2** | Set **A/AAAA → smtp.yourdomain.com** to the VPS IP and ask the host to set the **PTR** record to that same hostname | rDNS + forward match is Gmail’s first gate: without it mail is rejected or spam‑foldered even if SPF/DKIM are perfect.                                          |
| **3** | Publish an initial **SPF** record — `v=spf1 ip4:<VPS‑IP> -all`                        | Takes 5 min to propagate and instantly removes the “not authorized IP” error you saw.                                                                           |
| **4** | **Deploy the relay binary**, run your `swaks` test again                             | You should now see `delivered …` in the log and the message arrive in Gmail **Spam**. That proves network + SPF + rDNS are OK before you add cryptography.     |
| **5** | Add **DKIM signing** to the relay (small Go patch) and publish the **selector TXT**  | With SPF + DKIM aligned, Gmail flips DMARC to “pass” and the message usually jumps from Spam to Inbox within a few sends.                                       |
| **6** | Re‑test with **mail‑tester.com** until you hit 10/10                                 | That report verifies SPF, DKIM, rDNS, proper headers, and checks common spam signatures.                                                                        |

Once at 10/10:

* open Gmail Postmaster Tools for reputation,
* add **DMARC** (`v=DMARC1; p=none`) to monitor alignment,
* and start warming up the IP (low volumes, real opt‑in traffic).

Ready to tackle **Step 5 (DKIM signing)**? If so, I’ll drop in the ~40‑line Go patch and the DNS TXT syntax.
