from bs4 import BeautifulSoup
import requests
from urllib.parse import urlparse
import re

def get_link_preview(url):
    try:
        # Ensure the URL has a scheme
        parsed_url = urlparse(url)
        if not parsed_url.scheme:
            # Default to 'https' if the scheme is missing
            url = f'https://{url}'
            parsed_url = urlparse(url)

        # Validate the URL scheme
        if parsed_url.scheme not in ['http', 'https']:
            raise ValueError("Only 'http' and 'https' protocols are allowed.")

        # Perform the HTTP request
        response = requests.get(url)
        if response.status_code != 200:
            return {}

        soup = BeautifulSoup(response.text, 'html.parser')

        metadata = {
            "title": soup.find('meta', property='og:title') or
                     soup.find('meta', attrs={'name': 'twitter:title'})
                     or soup.title.string,
            "description": soup.find('meta', property='og:description') or
                           soup.find('meta', attrs={'name': 'twitter:description'}) or
                           soup.find('meta', attrs={'name': 'description'}),
            "img": soup.find('meta', property='og:image') or
                   soup.find('meta', attrs={'name': 'twitter:image'}) or
                   soup.find('link', rel='image_src') or
                   soup.find('img')['src'] if soup.find('img')
                   else None,
            "url": soup.find('meta', property='og:url') or url,
            "sitename": parsed_url.hostname #soup.find('meta', property='og:site_name')
        }

        # Clean up metadata
        for key, value in metadata.items():
            if value and hasattr(value, 'get'):
                metadata[key] = value.get('content')
        return metadata
    except Exception as e:
        raise Exception(e)
        #return {}



def youtube_url(url):
    if "watch?" in url:
        res = re.search(r"\?v=([^\/=&]+)", url)
        return f"https://img.youtube.com/vi/{res.group(1)}/0.jpg"
    else:
        res = re.search(r"\/([^\/]+)\?", url)
        return f"https://img.youtube.com/vi/{res.group(1)}/0.jpg"



def test(url):
        # Perform the HTTP request
        response = requests.get(url)
        if response.status_code != 200:
            return {}

        soup = BeautifulSoup(response.text, 'html.parser')


        if "youtu.be/" in url or "youtube.com/" in url:
            return youtube_url(url)

        # Reddit post image
        # return soup.find('img', id="post-image")["src"]
        # Reddit post multiple images
        #return soup.find('gallery-carousel') -> forbidden

        # Reddit youtube image
        embedded_link = [i for i in str(soup.find('shreddit-embed')).split(" ") if i.startswith("src") and "youtube.com" in i]
        if len(embedded_link) >0:
            return youtube_url(embedded_link[0])




if __name__ == "__main__":
    url1 = "https://www.youtube.com/watch?v=-FI4zp7jEso"

    url20 = "https://www.reddit.com/r/Finanzen/comments/1nhhohn/ich_habe_wieder_arbeit_bin_etwas_planlos_was_ich/" # only text
    url21 = "https://www.reddit.com/r/Finanzen/comments/1nh93j6/rossmann_otto_und_mediamarkt_wollen_wero_f%C3%BCr/" # link with image
    url22 = "https://www.reddit.com/r/Finanzen/comments/1nexlq1/wer_von_euch_war_das/" # only image
    url23 = "https://www.reddit.com/r/Finanzen/comments/1n97ax3/was_machen_die_menschen_da_%C3%BCberhaupt/" # image and text

    # url2 = "https://www.reddit.com/r/Finanzen/comments/1kcgea3/der_preis_des_d%C3%B6ners_zdfreportage/"
    # url3 = "https://www.reddit.com/r/Damnthatsinteresting/comments/1khxtwt/the_cpr_training_mannequins_face_is_originally/"
    url4 = "https://www.reddit.com/r/Fauxmoi/comments/1khwq2a/new_pope_leo_xiv_cardinal_robert_prevost_has_is/" # gallery
    # url5 = "https://www.reddit.com/r/soccercirclejerk/comments/1khz0ou/goat_goating_subs_closing/"
    url6 = "https://www.youtube.com/watch?v=rbkkxqghGNo"
    # print(get_link_preview(url3))
    print(test(url23))
